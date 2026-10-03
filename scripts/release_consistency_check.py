#!/usr/bin/env python3
"""发版一致性校验（Job000130 固化为标准流程的最后关卡）。
逐项核验六大渠道版本号一致：GitHub 仓库/标签、Docker Hub tag、系统内 /version、
操作手册（文档版+应用内）、versionHistory、部署模板。用法: python release_consistency_check.py <版本>"""
import json
import os
import re
import shutil
import subprocess
import sys
import urllib.request

import paramiko

VERSION = sys.argv[1] if len(sys.argv) > 1 else "1.7.1"
# 仓库根定位：从本文件位置向上两级（scripts/ -> 仓库根），
# 不再硬编码个人工作区绝对路径（换机器/换人 clone 也能跑）。
WORK = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
CFG = WORK + "/.workbuddy/ssh_config.json"


def _find_git():
    """定位 git 可执行文件：优先 PATH，其次 Windows 便携版 Git。

    原实现硬编码了个人机器的 PortableGit 绝对路径，换机器即失效。
    这里做逐级回退，找不到时返回 "git" 交给 subprocess 自行报错（信息更清楚）。
    """
    found = shutil.which("git")
    if found:
        return found
    portable = os.path.expanduser(
        "~/.workbuddy/binaries/PortableGit/versions")
    if os.path.isdir(portable):
        for root, _dirs, files in os.walk(portable):
            if "git.exe" in files:
                return os.path.join(root, "git.exe")
    return "git"


GIT = _find_git()

passed, failed = [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(f"{'PASS' if ok else 'FAIL'} {name}" + (f" | {detail}" if detail else ""))


# 1. GitHub 远端 main 与 tag
out = subprocess.run([GIT, "ls-remote", "origin", "main", f"v{VERSION}"],
                     capture_output=True, cwd=WORK, timeout=60).stdout.decode()
main_sha = tag_sha = ""
for ln in out.splitlines():
    if ln.endswith("refs/heads/main"):
        main_sha = ln.split()[0]
    if ln.endswith(f"refs/tags/v{VERSION}"):
        tag_sha = ln.split()[0]
check(f"GitHub main 存在", bool(main_sha), main_sha[:10])
check(f"GitHub tag v{VERSION} 存在", bool(tag_sha), tag_sha[:10])
# 发版后 main 可继续前进（修复类 commit），tag 只须是 main 的祖先
ancestor = subprocess.run([GIT, "merge-base", "--is-ancestor", f"v{VERSION}", "origin/main"],
                          capture_output=True, cwd=WORK, timeout=60).returncode == 0
check("GitHub tag v" + VERSION + " 是 main 祖先", ancestor and bool(tag_sha), f"main={main_sha[:10]} tag={tag_sha[:10]}")

# 2. 本仓库版本文件
ver_file = open(WORK + "/VERSION", encoding="utf-8").read().strip()
check("仓库 VERSION 文件", ver_file == VERSION, ver_file)
pkg = json.load(open(WORK + "/src/frontend/package.json", encoding="utf-8"))
check("package.json version", pkg.get("version") == VERSION, pkg.get("version"))
hist = subprocess.run(["node", "--input-type=module", "-e",
                       f"import('{WORK}/src/frontend/src/constants/versionHistory.js'.replace(/\\\\/g,'/')).then(m=>{{console.log(m.VERSION_HISTORY[0].version)}})"],
                      capture_output=True, timeout=60, shell=False)
# node ESM 路径在 Windows 下不稳——直接文本核验
hist_src = open(WORK + "/src/frontend/src/constants/versionHistory.js", encoding="utf-8").read()
check("versionHistory 头条目", f"version: '{VERSION}'" in hist_src.split("version: '")[1][:40].join(["version: '", ""]) or f"'{VERSION}'" in hist_src[:400],
      hist_src.splitlines()[8].strip() if len(hist_src.splitlines()) > 8 else "?")
syno = open(WORK + "/release/docker/docker-compose.synology.yml", encoding="utf-8").read()
check("群晖模板 tag", syno.count(VERSION) >= 10 and f":{VERSION}" in syno, f"命中 {syno.count(VERSION)} 处")
rn = WORK + f"/文档/Release_Notes_v{VERSION}.md"
import os
check("Release Notes 存在", os.path.exists(rn))
manual = open(WORK + "/文档/用户操作手册_v1.0.0.md", encoding="utf-8").read()
check("文档版手册尾注含 v" + VERSION, f"v{VERSION}" in manual)

# 3. Docker Hub（每镜像 1.7.1=latest）
for s in ["app", "worker", "db", "web"]:
    try:
        with urllib.request.urlopen(
                f"https://hub.docker.com/v2/repositories/warlocks/panomint-{s}/tags/{VERSION}", timeout=20) as r:
            d1 = json.load(r).get("digest", "")
    except Exception as e:
        d1 = f"ERR:{e}"
    try:
        with urllib.request.urlopen(
                f"https://hub.docker.com/v2/repositories/warlocks/panomint-{s}/tags/latest", timeout=20) as r:
            d2 = json.load(r).get("digest", "")
    except Exception as e:
        d2 = f"ERR:{e}"
    check(f"Hub panomint-{s} {VERSION}=latest", d1 == d2 and d1.startswith("sha256"),
          d1[:20] if d1 == d2 else f"{d1[:16]} vs {d2[:16]}")

# 4. 系统内 /version + 容器产物
cfg = json.load(open(CFG, encoding="utf-8"))
ROOT_PW = cfg["users"]["root"]  # 仅内存使用，不打印
cli = paramiko.SSHClient()
cli.set_missing_host_key_policy(paramiko.AutoAddPolicy())
cli.connect(cfg["host"], port=cfg.get("port", 22), username="warlocks",
            password=cfg["users"]["warlocks"], timeout=15)


def run(cmd, timeout=60):
    """以 warlocks 身份执行（docker/curl 类命令用这个，无需提权）。"""
    _, out, _ = cli.exec_command(cmd, timeout=timeout)
    return out.read().decode("utf-8", "replace")


def su_run(cmd, timeout=60):
    """以 root 身份执行（读 root 属主文件时必须走这个）。

    为什么需要：运行栈源码树 /home/warlocks/pano-album/ 下的文件由 root 写入
    （VERSION 是 -rw-rw-r-- root root，手册 content-*.js 同理），others 无读权。
    而 cli 登录身份是 warlocks，直接 exec_command 会得到空 stdout + stderr
    「权限不够」→ .strip() 成空串 → 该断言误判 FAIL（v1.9.3 发版时固定报这 2 项）。

    实现要点（踩过坑，勿简化）：
      - 必须用 PTY（get_pty）：su 在无 PTY 下不弹密码提示，导致下面的密码注入
        逻辑等不到触发条件，直接返回空。
      - 必须用 shlex.quote 包裹命令：不能用 repr()（它生成单引号包裹，
        在 su -c 的二次解析里会被吞掉，导致命令根本没执行 —— 这正是本脚本
        上一版修失败的原因，18/19 反而掉到 17/19）。
      - 返回**最后一个非空行**：su 会话尾部可能带 shell 提示符残留。
    """
    import shlex
    import time as _time
    chan = cli.get_transport().open_session()
    chan.get_pty()
    chan.exec_command("su -c " + shlex.quote(cmd))
    buf = b""
    sent = False
    deadline = _time.time() + timeout
    while _time.time() < deadline:
        if chan.recv_ready():
            chunk = chan.recv(65536)
            if not chunk:
                break
            buf += chunk
            if not sent and (b"assword" in buf or "密码".encode() in buf):
                chan.send(ROOT_PW + "\n")
                sent = True
        elif chan.exit_status_ready():
            break
        else:
            _time.sleep(0.2)
    text = buf.decode("utf-8", "replace")
    lines = [l.strip() for l in text.splitlines()
             if l.strip() and "密码" not in l and "Password:" not in l]
    # ⚠️ 必须滤掉命令自身的错误输出：文件不存在时 cat 会往 stdout 打
    # 「cat: xxx: 没有那个文件或目录」，若不过滤，调用方会拿这句错误文本去
    # 比对版本号 —— 虽然仍会判 FAIL（内容不匹配），但 detail 会误导排障方向
    # （看起来像「版本号错了」，实为「文件没了」）。反向验证脚本 D 覆盖此场景。
    err_markers = ("没有那个文件", "权限不够", "Is a directory", "No such file",
                   "cannot ", "cannot open", "not found")
    lines = [l for l in lines if not any(m in l for m in err_markers)]
    return lines[-1] if lines else ""


try:
    body = run("curl -s http://127.0.0.1:8088/version")
    j = json.loads(body)
    check("系统内 /version", j.get("version") == VERSION, body.strip())
    https_ver = run("curl -sk --resolve panomint.warlocks.cn:443:127.0.0.1 https://panomint.warlocks.cn/version")
    check("HTTPS /version", json.loads(https_ver).get("version") == VERSION, https_ver.strip()[:60])
    idx_entry = re.search(r'assets/(index-[A-Za-z0-9_-]+\.js)', open(WORK + "/src/frontend/dist/index.html", encoding="utf-8").read()).group(1)
    # 本机侧用 hashlib 直算，**不调外部 sha256sum**：
    # Windows 上 subprocess 调 Git Bash 的 sha256sum 会踩 MSYS 路径转换
    # （argv 里的 C:/... 被改写，输出被污染成 \xNN 形式），实测拿到的是
    # `\9f6b86d4533cdae`（少了首字符）而非 `9f6b86d4533cdae0`。
    # hashlib 是纯 Python，无 shell 无路径改写，跨平台一致。
    import hashlib
    with open(os.path.join(WORK, "src", "frontend", "dist", "assets", idx_entry), "rb") as fh:
        dist_local = hashlib.sha256(fh.read()).hexdigest()[:16]
    dist_remote = run("docker exec pano-web sha256sum /usr/share/nginx/html/assets/" + idx_entry).split()[0][:16]
    check("web 容器 dist 指纹 = 本机构建", dist_local == dist_remote and bool(dist_local),
          f"{idx_entry} {dist_local} vs {dist_remote}")
    # 这两项读 root 属主文件 → 必须走 su_run（见其 docstring 的踩坑说明）
    srv_ver = su_run("cat /home/warlocks/pano-album/VERSION")
    check("服务器源码树 VERSION", srv_ver == VERSION, srv_ver)
    manual_in = su_run("grep -c 'Range 流式' /home/warlocks/pano-album/src/frontend/src/manual/content-basics.js")
    check("服务器应用内手册已同步", manual_in == "1", f"命中 {manual_in}")
    n = run("docker ps --format '{{.Names}}' | wc -l").strip()
    check("容器全 Up (11)", n == "11", n)
finally:
    cli.close()

print(f"\nCONSISTENCY: pass={len(passed)} fail={len(failed)}")
if failed:
    print("FAILED:", failed)
sys.exit(1 if failed else 0)
