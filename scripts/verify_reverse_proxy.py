#!/usr/bin/env python3
"""反代两组前缀规则的可执行核验（Job000040）。

把「nginx 两组前缀必须手工同步」变成机器检查。真源三方：
  后端 API 前缀  <- 解析 cmd/api/main.go 的 gin 路由注册（Group 前缀 + 方法字面量）
  前端 SPA 前缀  <- 解析 src/frontend/src/router/index.js 的 path 字面量
  nginx 两组前缀 <- 解析 docker/web/Dockerfile 内嵌 default.conf 的两个 location 正则

六条规则（任一违反 → FAIL）：
  C1 nginx 恰好存在两组前缀 location（导航判别组 + 纯 API 组），正则可解析
  C2 后端 API 前缀 ⊆ 组1 ∪ 组2        （缺 → 该 API 落到 SPA 回退拿到 index.html）
  C3 组1 == 前端段 ∩ 后端段（精确）  （缺 → 刷新该页拿 401 JSON；多 → 语义漂移）
  C4 组1 ∩ 组2 = ∅
  C5 /share OG location 存在且 `$arg_spa` 判别**书写在** `$og_bot` 之前
     （nginx if 按书写顺序求值，颠倒会让微信真人被困 OG 页 —— 配置注释自承是功能正确性）
  C6 PWA/媒体规则存在：sw.js no-cache、manifest MIME、mjs MIME（升级链路）

用法：
  python scripts/verify_reverse_proxy.py            # 核验，退出码 0/1
  python scripts/verify_reverse_proxy.py --selftest # 变异自证（注入漂移必须 FAIL）
"""
import argparse
import os
import re
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MAIN_GO = os.path.join(ROOT, "src", "backend", "cmd", "api", "main.go")
ROUTER_JS = os.path.join(ROOT, "src", "frontend", "src", "router", "index.js")
WEB_DOCKERFILE = os.path.join(ROOT, "docker", "web", "Dockerfile")

HTTP_METHODS = "GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS"
GROUP_RE = re.compile(r'(\w+)\s*:?=\s*(\w+)\.Group\("([^"]*)"')
ROUTE_RE = re.compile(r'(\w+)\.(?:' + HTTP_METHODS + r')\("([^"]*)"')
PATH_LITERAL_RE = re.compile(r'path:\s*[\'"]([^\'"]+)[\'"]')
NGINX_GROUP_RE = re.compile(r'location\s*~\s*\^/\(([^)]+)\)(?:\(/|\$\))')
SPA_SEG_EXCLUDE = {}  # 预留：已知豁免段


def first_segment(path):
    p = path.strip()
    if not p.startswith("/"):
        p = "/" + p
    seg = p.strip("/").split("/", 1)[0]
    return seg.split(":")[0] if seg else ""


def backend_prefixes(main_go_path):
    with open(main_go_path, encoding="utf-8") as f:
        text = f.read()
    group_prefix = {"r": ""}
    # 反复迭代以支持 Group 链（子组定义可能出现在父组之后）
    for _ in range(5):
        changed = False
        for m in GROUP_RE.finditer(text):
            var, parent, prefix = m.group(1), m.group(2), m.group(3)
            if parent in group_prefix and var not in group_prefix:
                group_prefix[var] = group_prefix[parent] + prefix
                changed = True
        if not changed:
            break
    segs = set()
    for m in ROUTE_RE.finditer(text):
        recv, lit = m.group(1), m.group(2)
        if recv not in group_prefix:
            continue
        full = group_prefix[recv] + lit
        seg = first_segment(full)
        if seg:
            segs.add(seg)
    return segs, group_prefix


def frontend_segments(router_path):
    with open(router_path, encoding="utf-8") as f:
        text = f.read()
    segs = set()
    for m in PATH_LITERAL_RE.finditer(text):
        lit = m.group(1)
        if not lit or lit.startswith(":"):
            continue
        seg = first_segment(lit)
        if seg and not seg.startswith(":"):
            segs.add(seg)
    return segs


def nginx_groups(dockerfile_path):
    with open(dockerfile_path, encoding="utf-8") as f:
        text = f.read()
    m = re.search(r"COPY <<'EOF' /etc/nginx/conf\.d/default\.conf(.*?)^EOF", text,
                  re.S | re.M)
    if not m:
        return None, None, text
    conf = m.group(1)
    groups = []
    for g in NGINX_GROUP_RE.finditer(conf):
        groups.append(tuple(p.strip() for p in g.group(1).split("|")))
    return groups, conf, text


def verify(main_go=MAIN_GO, router=ROUTER_JS, dockerfile=WEB_DOCKERFILE, quiet=False):
    def log(msg):
        if not quiet:
            print(msg)

    fails = []
    api_segs, group_vars = backend_prefixes(main_go)
    fe_segs = frontend_segments(router)
    groups, conf, _raw = nginx_groups(dockerfile)
    if groups is None:
        return ["C1: 无法从 Dockerfile 抽取内嵌 nginx 配置"]

    # C1：两组前缀 location
    if len(groups) != 2:
        fails.append(f"C1: 期望 2 组前缀 location，实得 {len(groups)}: {groups}")
        return fails
    nav, pure = set(groups[0]), set(groups[1])

    # C2：后端前缀全覆盖
    missing = api_segs - nav - pure
    if missing:
        fails.append(f"C2: 后端 API 前缀未进任一组（API 将拿到 index.html）: {sorted(missing)}")

    # C3：导航判别组 = 前端 ∩ 后端
    expect_nav = fe_segs & api_segs
    if nav != expect_nav:
        fails.append(
            f"C3: 导航组应为 前端∩后端={sorted(expect_nav)}，实得 {sorted(nav)}；"
            f"缺 {sorted(expect_nav - nav)} 多 {sorted(nav - expect_nav)}")

    # C4：两组不相交
    inter = nav & pure
    if inter:
        fails.append(f"C4: 两组前缀相交: {sorted(inter)}")

    # C5：share OG 判别顺序（$arg_spa 必须在 $og_bot 之前）
    share_m = re.search(
        r'location ~ \^/share/\(\?<share_token>[^}]+?\{(.*?)\n    \}', conf, re.S)
    if not share_m:
        fails.append("C5: 缺少 /share/ OG location")
    else:
        body = share_m.group(1)
        spa_m = re.search(r"if \(\$arg_spa\)", body)
        bot_m = re.search(r"if \(\$og_bot\)", body)
        if not (spa_m and bot_m and spa_m.start() < bot_m.start()):
            fails.append("C5: /share 内 $arg_spa 判别必须书写在 $og_bot 之前")

    # C6：PWA/媒体规则存在
    for name, pat in [
        ("sw.js no-cache", r"location = /sw\.js"),
        ("manifest MIME", r"location = /manifest\.webmanifest"),
        ("mjs MIME", r"location ~ \\.mjs\$"),
    ]:
        if not re.search(pat, conf):
            fails.append(f"C6: 缺少 {name} 规则")

    log(f"[verify_reverse_proxy] 后端 API 前缀 {len(api_segs)}: {sorted(api_segs)}")
    log(f"[verify_reverse_proxy] 前端 SPA 段 {len(fe_segs)}: {sorted(fe_segs)}")
    log(f"[verify_reverse_proxy] nginx 导航组: {sorted(nav)}")
    log(f"[verify_reverse_proxy] nginx 纯API组: {sorted(pure)}")
    return fails


def cmd_check(_args):
    fails = verify()
    if fails:
        print("[verify_reverse_proxy] FAIL:")
        for f_ in fails:
            print(f"    {f_}")
        return 1
    print("[verify_reverse_proxy] 六条规则全过: PASS")
    return 0


GOOD_MAIN_GO = '''
package main
func main() {
	r := gin.Default()
	r.POST("/auth/login", h.Login)
	authed := r.Group("", m)
	authed.GET("/media", h.List)
	authed.GET("/search", h.Search)
	authed.GET("/albums", h.Albums)
	admin := authed.Group("/admin", m)
	admin.GET("/users", h.Users)
	agentPlane := r.Group("/compute-nodes/agent", m)
	agentPlane.POST("/heartbeat", h.Beat)
}
'''
GOOD_ROUTER = '''
const routes = [
 { path: '/login', component: Login },
 { path: '/', children: [
    { path: 'timeline' },
    { path: 'search' },
    { path: 'albums' },
 ]},
]
'''
NGINX_TMPL = '''
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
    listen 80;
    location ~ ^/(search|albums)(/|$) {
        if ($spa_nav) { return 418; }
        proxy_pass http://api:8080;
    }
    location ~ ^/(auth|media|admin|compute-nodes)(/|$) {
        proxy_pass http://api:8080;
    }
    location ~ ^/share/(?<share_token>[A-Za-z0-9_-]+)/?$ {
        error_page 418 = @share_og;
        error_page 419 = @spa;
        if ($arg_spa) { return 419; }
        if ($og_bot)  { return 418; }
        try_files $uri $uri/ /index.html;
    }
    location = /sw.js { add_header Cache-Control "no-cache"; }
    location = /manifest.webmanifest { default_type application/manifest+json; }
    location ~ \\.mjs$ { default_type application/javascript; }
}
EOF
'''


def cmd_selftest(_args):
    failures = []
    cases = []

    def run_case(name, main_go, router, dockerfile, expect_fail):
        with tempfile.TemporaryDirectory() as td:
            mg = os.path.join(td, "main.go")
            rt = os.path.join(td, "index.js")
            df = os.path.join(td, "Dockerfile")
            for path, content in [(mg, main_go), (rt, router), (df, dockerfile)]:
                with open(path, "w", encoding="utf-8") as f:
                    f.write(content)
            fails = verify(main_go=mg, router=rt, dockerfile=df, quiet=True)
        got_fail = bool(fails)
        if got_fail != expect_fail:
            failures.append(f"{name}: 期望 fail={expect_fail} 实得 fail={got_fail} ({fails})")
        else:
            print(f"[verify_reverse_proxy] selftest {name}: 符合预期（fail={got_fail}）")

    # 一致样例 → PASS
    run_case("一致样例", GOOD_MAIN_GO, GOOD_ROUTER, NGINX_TMPL, expect_fail=False)
    # 后端新增 /geo 前缀未同步 nginx → C2 FAIL
    run_case("后端新前缀未同步",
             GOOD_MAIN_GO + '\nfunc reg(r *gin.RouterGroup){ r.GET("/geo/x", h.G) }\n',
             GOOD_ROUTER, NGINX_TMPL, expect_fail=True)
    # 前后端都有 /player 但未进导航组 → C3 FAIL
    run_case("共有前缀缺进导航组",
             GOOD_MAIN_GO + '\nfunc reg(authed *gin.RouterGroup){ authed.GET("/player/:id", h.P) }\n',
             GOOD_ROUTER.replace("'login'", "'login'\n ,{ path: '/player/:id' }"),
             NGINX_TMPL, expect_fail=True)
    # $arg_spa/$og_bot 顺序颠倒 → C5 FAIL
    bad_nginx = NGINX_TMPL.replace(
        "if ($arg_spa) { return 419; }\n        if ($og_bot)",
        "if ($og_bot) { return 418; }\n        if ($arg_spa)")
    run_case("share 判别顺序颠倒", GOOD_MAIN_GO, GOOD_ROUTER, bad_nginx, expect_fail=True)
    # 两组相交 → C4 FAIL
    inter_nginx = NGINX_TMPL.replace("^/(auth|media|admin|compute-nodes)(/|$)",
                                      "^/(auth|media|admin|compute-nodes|search)(/|$)")
    run_case("两组相交", GOOD_MAIN_GO, GOOD_ROUTER, inter_nginx, expect_fail=True)

    if failures:
        for f_ in failures:
            print(f"[verify_reverse_proxy] selftest FAIL: {f_}")
        return 1
    print("[verify_reverse_proxy] selftest 全部通过")
    return 0


def main():
    ap = argparse.ArgumentParser(description="反代两组前缀可执行核验")
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    if args.selftest:
        return cmd_selftest(args)
    return cmd_check(args)


if __name__ == "__main__":
    sys.exit(main())
