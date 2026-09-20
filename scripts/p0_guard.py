#!/usr/bin/env python3
"""P0 规则可执行门禁（Job000037）。

把 MVP 开发专家团的三条 P0 绝对规则做成机器检查：

  R1 emoji_icon          UI 代码中禁止 emoji 作功能图标（允许白名单文本符号与注释）
  R2 purple_pink_grad    禁止紫→粉渐变主视觉（#7C3AED/#A855F7/#EC4899 组合及 Indigo→Pink 渐变）
  R3 ai_template         禁止 AI 模板味文案/缓动（Lorem ipsum / Welcome to / Sign up today /
                         cubic-bezier(0.68,-0.55,0.265,1.55)）
  R4 hardcoded_color     禁止硬编码颜色（例外 #fff/#000；令牌定义层 src/styles/tokens.css 豁免）

R4 对存量违规采用**棘轮（ratchet）**口径：基线清单 scripts/p0_guard_baseline.json 逐文件冻结
当前计数，守卫保证「只减不增」——新增/上移即 FAIL，修复后 `--refresh-baseline` 收紧。
R1–R3 为零容忍：命中即 FAIL（当前仓库 0 命中，靠 --selftest 自证检测能力真实）。

用法：
  python scripts/p0_guard.py              # 默认扫描，超基线则退出码 1
  python scripts/p0_guard.py --refresh-baseline   # 修复存量后收紧基线
  python scripts/p0_guard.py --selftest   # 变异自证：注入违规样本必须被逮住
"""
import argparse
import json
import os
import re
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FRONTEND_SRC = os.path.join(ROOT, "src", "frontend", "src")
SCAN_EXT = (".vue", ".js", ".ts", ".css", ".html")
# 令牌定义层：颜色只允许出现在这里（设计 Token 落点）
COLOR_EXEMPT_FILES = {"src/styles/tokens.css"}
# 颜色白名单（P0 规则原文唯一例外 #fff/#000，含其 6 位写法）
COLOR_WHITELIST = {"#fff", "#ffffff", "#000", "#000000"}
# 文本符号白名单：✓✕ 类提示文案、⚠ 注释、星级/方向符号——非功能图标
EMOJI_ALLOWLIST = set("✓✔✕✗☐☑⚠★☆☀☁☂❄♠♣♥♦♩♪♫⚐⚑✈✉✎✚✜❌❎➕➖➗")
EMOJI_RE = re.compile(
    "[\U0001F300-\U0001FAFF"
    "\u2600-\u26FF"
    "\u2700-\u27BF"
    "\U0001F000-\U0001F02F"
    "\U0001F0A0-\U0001F0FF"
    "\U0001F100-\U0001F64F"
    "\U0001F680-\U0001F6FF"
    "\U0001F900-\U0001F9FF"
    "\U0001FA00-\U0001FA6F"
    "\U0001FA70-\U0001FAFF"
    "\u200D\u20E3"
    "\U000E0020-\U000E007F]"
)
# ⚠️ 命中带 FE0F 变体选择符的符号（如 ⚠️）时，先剥掉 FE0F 再查白名单
EMOJI_VARIATION = re.compile("[\uFE00-\uFE0F]")

GRADIENT_BANNED_HEXES = ("#7c3aed", "#a855f7", "#ec4899")
GRADIENT_RE = re.compile(r"linear-gradient\([^)]*#[0-9a-fA-F]{3,8}[^)]*#[0-9a-fA-F]{3,8}[^)]*\)")
AI_COPY_RES = [
    re.compile(r"Lorem\s+ipsum", re.I),
    re.compile(r"Welcome to( our)? app", re.I),
    re.compile(r"Sign up today", re.I),
    re.compile(r"0\.68,\s*-0\.55,\s*0\.265,\s*1\.55"),
]
HEX_RE = re.compile(r"#[0-9a-fA-F]{3,8}\b")

BASELINE_PATH = os.path.join(ROOT, "scripts", "p0_guard_baseline.json")


def iter_scan_files():
    for dirpath, _dirs, files in os.walk(FRONTEND_SRC):
        for name in files:
            if name.endswith(SCAN_EXT):
                yield os.path.relpath(os.path.join(dirpath, name), FRONTEND_SRC)


def scan():
    """返回 {rule: {relpath: [行号,...]}} 结构的违规表（只含违规行号）。"""
    hits = {"emoji": {}, "gradient": {}, "ai_copy": {}, "color": {}}
    for rel in sorted(iter_scan_files()):
        full = os.path.join(FRONTEND_SRC, rel)
        with open(full, encoding="utf-8") as f:
            try:
                text = f.read()
            except UnicodeDecodeError:
                continue
        lines = text.splitlines()
        # R1 emoji（白名单豁免；注释行豁免——注释是开发者面向，不是 UI 图标）
        for i, line in enumerate(lines, 1):
            stripped = EMOJI_VARIATION.sub("", line)
            code_part = stripped.split("//", 1)[0].split("/*", 1)[0]
            for m in EMOJI_RE.finditer(code_part):
                if m.group(0) not in EMOJI_ALLOWLIST:
                    hits["emoji"].setdefault(rel, []).append(i)
                    break
        # R2 紫粉渐变：整个文件查渐变函数里同时含 banned hex（大小写不敏感）
        low = text.lower()
        for m in GRADIENT_RE.finditer(low):
            body = m.group(0)
            if any(h in body for h in GRADIENT_BANNED_HEXES):
                ln = low.count("\n", 0, m.start()) + 1
                hits["gradient"].setdefault(rel, []).append(ln)
        # R3 AI 模板味
        for i, line in enumerate(lines, 1):
            if any(r.search(line) for r in AI_COPY_RES):
                hits["ai_copy"].setdefault(rel, []).append(i)
        # R4 硬编码颜色（令牌层豁免；白名单豁免；注释行豁免）
        if rel not in COLOR_EXEMPT_FILES:
            for i, line in enumerate(lines, 1):
                code_part = line.split("//", 1)[0]
                for m in HEX_RE.finditer(code_part):
                    if m.group(0).lower() not in COLOR_WHITELIST:
                        hits["color"].setdefault(rel, []).append(i)
                        break
    return hits


def count_by_file(hits, rule):
    return {p: len(ls) for p, ls in hits.get(rule, {}).items()}


def load_baseline():
    if not os.path.exists(BASELINE_PATH):
        return {}
    with open(BASELINE_PATH, encoding="utf-8") as f:
        return json.load(f)


def cmd_check(_args):
    hits = scan()
    fails = []

    # R1–R3 零容忍
    for rule in ("emoji", "gradient", "ai_copy"):
        if hits[rule]:
            fails.append((rule, {p: ls for p, ls in hits[rule].items()}, "零容忍规则命中"))

    # R4 棘轮：逐文件计数不得超基线，且不得出现基线外新文件
    baseline = load_baseline()
    cur = count_by_file(hits, "color")
    over, new_files = [], []
    for path, n in cur.items():
        if path not in baseline:
            new_files.append((path, n))
        elif n > baseline[path]:
            over.append((path, baseline[path], n))
    if over:
        fails.append(("color", over, "存量文件硬编码颜色数超基线"))
    if new_files:
        fails.append(("color", new_files, "新增文件含硬编码颜色"))

    # 打印报告
    total_color = sum(cur.values())
    print(f"[p0_guard] 扫描 {len(list(iter_scan_files()))} 个前端文件")
    print(f"[p0_guard] R1 emoji 图标违规: {sum(len(v) for v in hits['emoji'].values())} 处")
    print(f"[p0_guard] R2 紫粉渐变违规: {sum(len(v) for v in hits['gradient'].values())} 处")
    print(f"[p0_guard] R3 AI 模板味违规: {sum(len(v) for v in hits['ai_copy'].values())} 处")
    print(f"[p0_guard] R4 硬编码颜色: 存量 {total_color} 处（基线 "
          f"{sum(load_baseline().values()) if baseline else '未建立'}，只减不增）")
    if fails:
        for rule, items, why in fails:
            print(f"[p0_guard] FAIL [{rule}] {why}:")
            for it in items:
                print(f"    {it}")
        print("[p0_guard] 结论: FAIL")
        return 1
    print("[p0_guard] 结论: PASS")
    return 0


def cmd_refresh_baseline(_args):
    hits = scan()
    cur = count_by_file(hits, "color")
    with open(BASELINE_PATH, "w", encoding="utf-8") as f:
        json.dump(cur, f, ensure_ascii=False, indent=1, sort_keys=True)
    print(f"[p0_guard] 基线已收紧：{len(cur)} 个文件共 {sum(cur.values())} 处硬编码颜色 -> {BASELINE_PATH}")
    return 0


SELFTEST_SAMPLES = {
    "emoji": "<template><button>🔍 搜索</button></template>",
    "gradient": ".x{background:linear-gradient(135deg,#7C3AED,#A855F7,#EC4899)}",
    "ai_copy": "<template><h1>Welcome to Our App</h1><p>Sign up today</p></template>",
    "color": ".x{color:#0f172a}",
}


def cmd_selftest(_args):
    """变异自证：每类违规样本必须被对应规则逮住；干净样本必须全过。"""
    failures = []
    for rule, sample in SELFTEST_SAMPLES.items():
        with tempfile.TemporaryDirectory() as td:
            # 写样本进临时目录，临时替换扫描根
            sub = os.path.join(td, "src", "frontend", "src", "comp")
            os.makedirs(sub)
            fname = "Probe.vue" if "template" in sample or rule == "emoji" else "probe.css"
            with open(os.path.join(sub, fname), "w", encoding="utf-8") as f:
                f.write(sample)
            global FRONTEND_SRC
            saved = FRONTEND_SRC
            FRONTEND_SRC = os.path.join(td, "src", "frontend", "src")
            try:
                hits = scan()
            finally:
                FRONTEND_SRC = saved
        key = {"emoji": "emoji", "gradient": "gradient", "ai_copy": "ai_copy", "color": "color"}[rule]
        if not hits[key]:
            failures.append(f"{rule}: 注入样本未被检出")
        else:
            print(f"[p0_guard] selftest {rule}: 注入样本已被检出 ✓")
    # 干净样本（白名单符号 + 注释 + 白名单颜色）不得误报
    clean = (
        "<template><p>AI 建议，点 ✓ 接受</p></template>\n"
        "// ⚠️ 注释里的符号不算图标\n"
        "<style>.a{color:#fff;background:#000}</style>\n"
    )
    with tempfile.TemporaryDirectory() as td:
        sub = os.path.join(td, "src", "frontend", "src", "comp")
        os.makedirs(sub)
        with open(os.path.join(sub, "Clean.vue"), "w", encoding="utf-8") as f:
            f.write(clean)
        saved = FRONTEND_SRC
        FRONTEND_SRC = os.path.join(td, "src", "frontend", "src")
        try:
            hits = scan()
        finally:
            FRONTEND_SRC = saved
    false_pos = {r: v for r, v in hits.items() if v}
    if false_pos:
        failures.append(f"干净样本误报: {false_pos}")
    else:
        print("[p0_guard] selftest clean: 白名单/注释/豁免色零误报 ✓")
    if failures:
        for f_ in failures:
            print(f"[p0_guard] selftest FAIL: {f_}")
        return 1
    print("[p0_guard] selftest 全部通过（检测能力真实）")
    return 0


def main():
    ap = argparse.ArgumentParser(description="P0 规则可执行门禁")
    ap.add_argument("--refresh-baseline", action="store_true", help="修复存量后收紧 R4 基线")
    ap.add_argument("--selftest", action="store_true", help="变异自证")
    args = ap.parse_args()
    if args.selftest:
        return cmd_selftest(args)
    if args.refresh_baseline:
        return cmd_refresh_baseline(args)
    return cmd_check(args)


if __name__ == "__main__":
    sys.exit(main())
