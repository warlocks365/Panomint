#!/usr/bin/env python3
"""前端代码组织门禁（Job000038）。

两条组织纪律的机器检查：

  O1 单文件 ≤300 行（MVP 工程规范）——**棘轮**口径：
     存量超大文件登记入基线（scripts/frontend_org_baseline.json），守卫保证
     「不新增超大文件 + 已登记文件不得变长」；拆分修复后 `--refresh-baseline` 收紧。
  O2 图标库不混用——本项目图标为**内联 SVG 单一约定**（package.json 无图标库依赖），
     故守卫钉死两条：① package.json 不得新增图标库依赖（已知库名清单）；
     ② src 内不得 import/require 任何图标库包；③ index.html 不得引入外部图标/字体 CDN。

用法：
  python scripts/frontend_org_guard.py              # 扫描，违反棘轮/钉死则退出码 1
  python scripts/frontend_org_guard.py --refresh-baseline   # 拆分后收紧基线
  python scripts/frontend_org_guard.py --selftest   # 变异自证
"""
import argparse
import json
import os
import re
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FE = os.path.join(ROOT, "src", "frontend")
FE_SRC = os.path.join(FE, "src")
SCAN_EXT = (".vue", ".js", ".ts")
MAX_LINES = 300

BASELINE_PATH = os.path.join(ROOT, "scripts", "frontend_org_baseline.json")

# 已知图标库 npm 包名（命中即违反「锁定一套」原则；本项目=内联 SVG，应为 0）
ICON_PACKAGES = {
    "lucide-vue-next", "lucide-react", "@mdi/font", "@mdi/js", "@heroicons/vue",
    "@heroicons/react", "@iconify/vue", "@iconify/react", "vue-feather", "feather-icons",
    "bootstrap-icons", "@fortawesome/fontawesome-free", "@fortawesome/vue-fontawesome",
    "ant-design-vue", "@ant-design/icons", "@ant-design/icons-vue", "@vicons/material",
    "@vicons/ionicons5", "@vicons/fa", "ionicons", "material-icons", "@mui/icons-material",
    "react-icons", "vue-material-design-icons", "oh-vue-icons", "@icon-park/vue",
    "unplugin-icons", "@iconscout/unicons",
}
IMPORT_RE = re.compile(r'(?:import|require)\s*\(?\s*[\'"]([^\'"]+)[\'"]')
CDN_RE = re.compile(r'(?:src|href)\s*=\s*[\'"]https?://[^\'"]+[\'"]')


def iter_source_files():
    for dirpath, _dirs, files in os.walk(FE_SRC):
        for name in files:
            if name.endswith(SCAN_EXT):
                yield os.path.relpath(os.path.join(dirpath, name), FE_SRC)


def line_counts():
    counts = {}
    for rel in iter_source_files():
        with open(os.path.join(FE_SRC, rel), encoding="utf-8") as f:
            counts[rel] = sum(1 for _ in f)
    return counts


def load_baseline():
    if not os.path.exists(BASELINE_PATH):
        return {}
    with open(BASELINE_PATH, encoding="utf-8") as f:
        return json.load(f)


def icon_violations():
    v = []
    pkg_path = os.path.join(FE, "package.json")
    with open(pkg_path, encoding="utf-8") as f:
        pkg = json.load(f)
    deps = set(pkg.get("dependencies", {})) | set(pkg.get("devDependencies", {}))
    for p in sorted(deps & ICON_PACKAGES):
        v.append(f"package.json 依赖图标库: {p}")
    for rel in iter_source_files():
        full = os.path.join(FE_SRC, rel)
        with open(full, encoding="utf-8") as f:
            text = f.read()
        for m in IMPORT_RE.finditer(text):
            mod = m.group(1)
            head = mod if not mod.startswith("@") else "/".join(mod.split("/")[:2])
            head = head.split("/")[0] if not mod.startswith("@") else head
            for ip in ICON_PACKAGES:
                if mod == ip or mod.startswith(ip + "/"):
                    ln = text.count("\n", 0, m.start()) + 1
                    v.append(f"{rel}:{ln} import 图标库: {mod}")
                    break
    idx = os.path.join(FE, "index.html")
    if os.path.exists(idx):
        with open(idx, encoding="utf-8") as f:
            for i, line in enumerate(f, 1):
                for m in CDN_RE.finditer(line):
                    v.append(f"index.html:{i} 外部资源: {m.group(0)}")
    return v


def cmd_check(_args):
    counts = line_counts()
    baseline = load_baseline()
    fails = []

    over_new = {p: n for p, n in counts.items() if n > MAX_LINES and p not in baseline}
    over_grow = {p: (baseline[p], n) for p, n in counts.items()
                 if p in baseline and n > baseline[p]}
    over_shrink = sum(1 for p, n in counts.items() if p in baseline and n < baseline[p])
    if over_new:
        fails.append(("O1", over_new, f"新增超 {MAX_LINES} 行文件"))
    if over_grow:
        fails.append(("O1", over_grow, "已登记超大文件变长"))

    icons = icon_violations()
    if icons:
        fails.append(("O2", icons, "图标库混用/外部图标资源"))

    big = sum(1 for n in counts.values() if n > MAX_LINES)
    print(f"[frontend_org] 扫描 {len(counts)} 个源文件；超 {MAX_LINES} 行: {big} 个"
          f"（基线登记 {len(baseline)} 个，棘轮：不新增、不变长；已改善 {over_shrink} 个待收紧）")
    print(f"[frontend_org] O2 图标库检查: {len(icons)} 处违规（锁定=内联 SVG）")
    if fails:
        for rule, items, why in fails:
            print(f"[frontend_org] FAIL [{rule}] {why}:")
            for it in (items.items() if isinstance(items, dict) else items):
                print(f"    {it}")
        print("[frontend_org] 结论: FAIL")
        return 1
    print("[frontend_org] 结论: PASS")
    return 0


def cmd_refresh_baseline(_args):
    counts = line_counts()
    big = {p: n for p, n in counts.items() if n > MAX_LINES}
    with open(BASELINE_PATH, "w", encoding="utf-8") as f:
        json.dump(big, f, ensure_ascii=False, indent=1, sort_keys=True)
    print(f"[frontend_org] 基线已收紧：{len(big)} 个超大文件（合计 {sum(big.values())} 行）")
    return 0


def cmd_selftest(_args):
    failures = []
    with tempfile.TemporaryDirectory() as td:
        sub = os.path.join(td, "src", "frontend", "src")
        os.makedirs(sub)
        with open(os.path.join(sub, "Big.vue"), "w") as f:
            f.write("\n".join(f"<!-- line {i} -->" for i in range(301)))
        with open(os.path.join(sub, "Icons.vue"), "w") as f:
            f.write("import { Menu } from 'lucide-vue-next'\n")
        idx = os.path.join(td, "src", "frontend", "index.html")
        with open(idx, "w") as f:
            f.write('<html><link rel="stylesheet" href="https://cdn.example.com/icons.css"></html>')
        with open(os.path.join(td, "src", "frontend", "package.json"), "w") as f:
            json.dump({"dependencies": {"vue": "^3.5.12", "lucide-vue-next": "^0.4xx"}}, f)
        # 干净文件
        with open(os.path.join(sub, "Ok.vue"), "w") as f:
            f.write("<!-- 小文件 -->\n")
        global FE, FE_SRC
        saved_fe, saved_src = FE, FE_SRC
        FE, FE_SRC = os.path.join(td, "src", "frontend"), os.path.join(td, "src", "frontend", "src")
        try:
            counts = line_counts()
            icons = icon_violations()
        finally:
            FE, FE_SRC = saved_fe, saved_src
        if "Big.vue" not in {p for p, n in counts.items() if n > MAX_LINES}:
            failures.append("301 行文件未被 O1 识别")
        joined = "\n".join(icons)
        if "lucide-vue-next" not in joined:
            failures.append("图标库 import 未被 O2 识别")
        if "cdn.example.com" not in joined:
            failures.append("外部图标 CDN 未被 O2 识别")
    if failures:
        for f_ in failures:
            print(f"[frontend_org] selftest FAIL: {f_}")
        return 1
    print("[frontend_org] selftest 通过：301 行文件/图标库 import/外部 CDN 均检出")
    return 0


def main():
    ap = argparse.ArgumentParser(description="前端代码组织门禁")
    ap.add_argument("--refresh-baseline", action="store_true")
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    if args.selftest:
        return cmd_selftest(args)
    if args.refresh_baseline:
        return cmd_refresh_baseline(args)
    return cmd_check(args)


if __name__ == "__main__":
    sys.exit(main())
