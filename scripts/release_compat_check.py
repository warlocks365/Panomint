#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""发版兼容性检查（R-A 数据库兼容 + R-B 构建文件变更提醒）。

对应《二期规划_数据库兼容与在线升级_v1.0.md》第 4 节。
并入发版链，与 release/release_consistency_check.py 同级；两者全 PASS 才允许继续。

用法：
    python scripts/release_compat_check.py --diff-from v1.9.4
    python scripts/release_compat_check.py --migrations-only
    python scripts/release_compat_check.py --build-files-only --diff-from v1.9.4

设计要点（都是踩过的坑）：
1. **先剥注释再匹配**。否则 SQL 注释里出现的 `DROP COLUMN` 字样会造成假阳性——
   本项目曾因grep 子串假阳性派出了错误的测试修改。
2. **构建文件比对走 git diff**，不另建指纹清单：git 是唯一真源，且天然带上下��。
3. 退出码严格分离，绝不把 build 的退出码吞进管道。
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
MIG_DIR = os.path.join(ROOT, "src", "backend", "migrations")

# ---------------------------------------------------------------- R-A 规则

# 阻断级：不可逆的数据丢失（A2）
BLOCKING = [
    (r"\bDROP\s+TABLE\b", "DROP TABLE"),
    (r"\bTRUNCATE\b", "TRUNCATE"),
    (r"\bRENAME\s+COLUMN\b", "RENAME COLUMN"),
    (r"\bALTER\s+TABLE\b[^;]*?\bRENAME\b[^;]*?\bTO\b", "RENAME TO"),
]

# 注意级：可能破坏兼容，但不必然丢数据，需人工确认
ADVISORY = [
    (r"\bDROP\s+COLUMN\b", "DROP COLUMN（若是两段式迁移的第二段则 OK）"),
    (r"\bDROP\s+INDEX\b", "DROP INDEX（索引可重建，通常无害）"),
    (r"\bDROP\s+CONSTRAINT\b", "DROP CONSTRAINT（约束可重加，但期间无保护）"),
    (r"\bALTER\s+(COLUMN\s+)?[\"`]?\w+[`\"]?\s+TYPE\b", "ALTER COLUMN ... TYPE（须人工确认是扩大而非收窄）"),
]

# 列类型扩大方向（如 varchar(50) -> varchar(200)、vector(128) -> vector(512)）
WIDENING = re.compile(
    r"\b(?:var|char|nvar|vector|bpchar)\s*\(\s*(\d+)\s*\)\s*$")

# 迁移头部元信息（A5）
MIN_VER = re.compile(r"--\s*min-app-version\s*[:：]\s*v?\d+\.\d+\.\d+", re.I)

# ADD COLUMN 是否带 NOT NULL 且无 DEFAULT（A1）
ADD_COL = re.compile(
    r"ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?[\"`]?(\w+)[`\"]?\s+([^,;]+)", re.I)


def strip_sql_comments(sql: str) -> str:
    """剥掉 -- 行注释与 /* */ 块注释，避免注释里的关键字造成假阳性"""
    out = []
    i, n = 0, len(sql)
    while i < n:
        if sql.startswith("--", i):
            j = sql.find("\n", i)
            i = n if j < 0 else j
        elif sql.startswith("/*", i):
            j = sql.find("*/", i + 2)
            i = n if j < 0 else j + 2
            out.append(" ")
        else:
            out.append(sql[i])
            i += 1
    return "".join(out)


def goose_up_body(sql: str) -> str:
    """只取 `-- +goose Up` 段。

    🔴 这是本检查器最容易犯的错：goose 启动**只执行 up**，
    `-- +goose Down` 里的 DROP 语句永远不会跑。
    分析整个文件会把回滚定义误判为破坏性操作（实测 83 条假阳性全部来自 Down 段）。
    """
    m = re.search(r"^\s*--\s*\+goose\s+Down\s*$", sql, re.I | re.M)
    return sql[: m.start()] if m else sql


def check_migrations() -> tuple:
    """返回 (passed: bool, lines: list[str])"""
    lines = []
    fails = 0
    warns = 0
    if not os.path.isdir(MIG_DIR):
        return False, ["[FAIL] 迁移目录不存在: %s" % MIG_DIR]

    files = sorted(f for f in os.listdir(MIG_DIR) if f.endswith(".sql"))
    lines.append("[INFO] 迁移总数 %d（仅分析 -- +goose Up 段）" % len(files))
    if not files:
        return True, lines

    blocking_hits = []
    advisory_hits = []
    missing_ver = []
    bad_new_col = []

    for f in files:
        p = os.path.join(MIG_DIR, f)
        with open(p, encoding="utf-8") as fh:
            raw = fh.read()
        body = strip_sql_comments(goose_up_body(raw))

        for pat, label in BLOCKING:
            for m in re.finditer(pat, body, re.I):
                seg = body[max(0, m.start() - 60): m.end() + 60].replace("\n", " ")
                blocking_hits.append("%s: %s  ← …%s…" % (f, label, seg.strip()))

        for pat, label in ADVISORY:
            for m in re.finditer(pat, body, re.I):
                seg = body[max(0, m.start() - 60): m.end() + 60].replace("\n", " ")
                advisory_hits.append("%s: %s  ← …%s…" % (f, label, seg.strip()))

        if not MIN_VER.search(raw):
            missing_ver.append(f)

        for m in ADD_COL.finditer(body):
            col, rest = m.group(1), m.group(2)
            has_not_null = re.search(r"\bNOT\s+NULL\b", rest, re.I)
            has_default = re.search(r"\bDEFAULT\b", rest, re.I)
            if has_not_null and not has_default:
                bad_new_col.append("%s: 列 `%s` 为 NOT NULL 且无 DEFAULT" % (f, col))

    if blocking_hits:
        fails += 1
        lines.append("[FAIL] 阻断级破坏性操作 %d 处（A2 禁止，需两段式：本期只加，下期才删）"
                     % len(blocking_hits))
        for h in blocking_hits[:15]:
            lines.append("       - " + h)
    else:
        lines.append("[PASS] 无 DROP TABLE / TRUNCATE / RENAME（阻断级）")

    if advisory_hits:
        warns += 1
        lines.append("[WARN] 注意级 %d 处 —— 多数无害，但需人工逐条确认：" % len(advisory_hits))
        seen = {}
        for h in advisory_hits:
            key = h.split(":")[1].split("  ←")[0].strip()
            seen.setdefault(key, []).append(h.split(":")[0])
        for label, fs in sorted(seen.items(), key=lambda kv: -len(kv[1])):
            lines.append("       · %-52s %d 处（%s%s）"
                         % (label, len(fs), fs[0], " …" if len(fs) > 1 else ""))
    else:
        lines.append("[PASS] 无 DROP COLUMN / DROP INDEX / ALTER COLUMN TYPE（注意级）")

    if bad_new_col:
        fails += 1
        lines.append("[FAIL] 新增列 NOT NULL 且无 DEFAULT %d 处（A1 违反，老数据写不进去）"
                     % len(bad_new_col))
        for h in bad_new_col[:15]:
            lines.append("       - " + h)
    else:
        lines.append("[PASS] 新增列均为 nullable 或带 DEFAULT")

    if missing_ver:
        fails += 1
        lines.append("[FAIL] %d 个迁移缺少 `-- min-app-version: x.y.z` 声明（A5）"
                     % len(missing_ver))
        lines.append("       - " + ", ".join(missing_ver[:6]) + (" …" if len(missing_ver) > 6 else ""))
    else:
        lines.append("[PASS] 全部迁移均声明 min-app-version")

    lines.append("[INFO] 阻断 %d 项 / 注意 %d 项" % (fails, warns))
    return fails == 0, lines


# ---------------------------------------------------------------- R-B 规则

BUILD_FILES = [
    ("docker-compose.yml", "high", "编排文件：变更可能需手工调整端口、卷、环境变量"),
    ("docker/api/Dockerfile", "high", "api 镜像构建：变更可能导致升级后无法重建"),
    ("docker/web/Dockerfile", "high", "web 镜像构建"),
    ("docker/db/Dockerfile", "high", "db 镜像构建"),
    ("docker/worker/Dockerfile", "high", "worker 镜像构建"),
    (".env.example", "warn", "环境变量样例：新增变量可能需用户补配置"),
]
LEVEL_MARK = {"high": "🔴 [高危]", "warn": "🟡 [注意]"}


def git_changed(ref: str) -> tuple:
    """返回 (set(变更文件), 错误信息)"""
    try:
        r = subprocess.run(["git", "diff", "--name-only", "%s..HEAD" % ref],
                           cwd=ROOT, capture_output=True, text=True, timeout=60)
    except Exception as exc:
        return set(), "调用 git 失败: %s" % exc
    if r.returncode != 0:
        return set(), "git diff %s..HEAD 失败: %s" % (ref, (r.stderr or "").strip()[:200])
    return {l.strip() for l in r.stdout.splitlines() if l.strip()}, ""


def check_build_files(ref: str) -> tuple:
    lines = []
    warns = 0
    changed, err = git_changed(ref)
    if err:
        return False, ["[FAIL] " + err]

    lines.append("[INFO] 与 %s 对比，共 %d 个文件变更" % (ref, len(changed)))

    high, warn = [], []
    for path, level, desc in BUILD_FILES:
        if path in changed:
            (high if level == "high" else warn).append(path)
            lines.append("  %s %-28s 已变更  —— %s" % (LEVEL_MARK[level], path, desc))

    for p in sorted(changed):
        if p.startswith("docker/") and p.endswith("Dockerfile") and p not in {b[0] for b in BUILD_FILES}:
            warn.append(p)
            lines.append("  🟡 [注意] %-28s 已变更（清单外新增的 Dockerfile）" % p)

    if high:
        lines.append("[FAIL] 🔴 %d 个高危构建文件变更（B2/B3：升级前必须提示用户，"
                     "否则用户不手工跟进就起不来）" % len(high))
        warns += 1
    elif warn:
        lines.append("[WARN] 🟡 %d 个需注意的构建文件变更" % len(warn))
    else:
        lines.append("[PASS] 构建文件无变更（B3 常规级）")

    lines.append("       提示：变更摘要需写入 update/latest.json 的 build_files_changed，"
                 "供在线升级界面直接读取（见规划 §5.2）")
    return warns == 0, lines


# ---------------------------------------------------------------- main
def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--diff-from", default="", help="对比基线，如 v1.9.4 或 1.9.3")
    ap.add_argument("--migrations-only", action="store_true")
    ap.add_argument("--build-files-only", action="store_true")
    args = ap.parse_args()

    all_ok = True

    if not args.build_files_only:
        ok, lines = check_migrations()
        all_ok = all_ok and ok
        print("=== 数据库兼容检查（R-A）===")
        for l in lines:
            print("  " + l)
        print("  RESULT: %s\n" % ("PASS" if ok else "FAILED"))

    if not args.migrations_only:
        if not args.diff_from:
            print("=== 构建文件变更检查（R-B）===")
            print("  [SKIP] 未指定 --diff-from，跳过（用法: --diff-from v1.9.4）")
        else:
            ok, lines = check_build_files(args.diff_from)
            all_ok = all_ok and ok
            print("=== 构建文件变更检查（R-B，基线 %s）===" % args.diff_from)
            for l in lines:
                print("  " + l)
            print("  RESULT: %s\n" % ("PASS" if ok else "FAILED"))

    print("=== 总判定: %s ===" % ("PASS —— 可继续发版" if all_ok else "FAILED —— 阻断发版"))
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
