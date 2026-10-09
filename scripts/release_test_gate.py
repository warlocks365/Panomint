#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""发版测试门禁（把「测试红」变成能拦住发版的东西）。

与 scripts/release_compat_check.py（R-A/R-B）、scripts/release_consistency_check.py（19 项）
同级，三者全PASS 才允许继续发版。

为什么必须有这个脚本
--------------------
改造前的实测事实：整个发版链**不跑任何测试**。
  - release/build.sh 只 `npm ci && npm run build`（构建，不测）
  - release_consistency_check.py 19 项只校验版本号在各文件间是否一致
  - release_compat_check.py 只读 SQL 与 git diff，不执行测试
于是「版本号一致但代码是坏的」可以畅通发版。本脚本补的就是这一段。

三条踩过坑的硬约束（勿简化）
--------------------------
1. **退出码绝不进管道**。`go test ./... | tail -40` 会让失败也返回 0（管道退出码取
   末命令）。本项目已因这个假绿灯栽过两次。本脚本用 Popen.wait() 取真实 returncode，
   输出走tee 落临时文件只为截取失败现场，不参与判定。
2. **`-count=1` 必须带**。Go 测试结果默认进构建缓存；不带 `-count=1` 时，改动源码后
   仍可能拿到上一次的绿灯。同理前端 vitest 走 `vitest run`（单次运行，非 watch）。
3. **工具链缺失 = FAIL，不是 SKIP**。找不到 go / node 时报FAIL 并给出补救命令。
   「环境问题所以跳过」是门禁最典型的失效形态：绿灯回来了，红的从来没人看见。

用法
----
    python scripts/release_test_gate.py                    # 后端 + 前端（发版链默认）
    python scripts/release_test_gate.py --backend-only
    python scripts/release_test_gate.py --frontend-only
    python scripts/release_test_gate.py --install-deps      # 前端缺 node_modules 时先 npm ci

退出码：0 = 全PASS；1 = 有阻断项（红）；2 = 门禁自身无法运行（工具链缺失等）。
"""
from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
BACKEND = os.path.join(ROOT, "src", "backend")
FRONTEND = os.path.join(ROOT, "src", "frontend")

# 后端默认测全部包。取舍依据见脚本末尾 run_backend() 的注释：
# 2026-10-09 实测 `go test -count=1 ./...` 在本机55 个包全绿（EXIT=0），
# 因此无需排除任何包。若将来某包因缺外部依赖必然失败，正确做法是给它补测试替身
# 或用 --backend-packages 显式排除并在下方登记理由——**不要**为了让门禁变绿而删断言。
BACKEND_PACKAGES = ["./..."]

# 失败时回显的输出行数（仅用于给人看，不参与判定）
TAIL_LINES = 60


# ----------------------------------------------------------------- 工具链定位
def find_tool(name: str) -> str:
    """定位可执行文件。Windows 便携版在 ~/.workbuddy/binaries/ 下，不在 PATH。

    与 release_consistency_check._find_git() 同思路：找不到就返回 None，
    由调用方报 FAIL——**绝不返回一个猜测路径让 subprocess 静默失败**。
    """
    found = shutil.which(name)
    if found:
        return found
    base = os.path.expanduser("~/.workbuddy/binaries")
    if not os.path.isdir(base):
        return None
    for sub in sorted(os.listdir(base), reverse=True):
        root = os.path.join(base, sub, "versions")
        if not os.path.isdir(root):
            continue
        for ver in sorted(os.listdir(root), reverse=True):
            for exe in (name, name + ".exe", name + ".cmd"):
                cand = os.path.join(root, ver, exe)
                if os.path.isfile(cand):
                    return cand
    return None


# ----------------------------------------------------------------- 执行器
def run_streaming(cmd, cwd, label):
    """执行命令，实时回显输出，返回 (returncode, 输出文本)。

    🔴 绝不写成`cmd | tail`：那样 returncode 取的是 tail 的退出码，失败会被洗成 0。
    这里用 Popen 直连 stdout/stderr，边读边打印，同时写入临时文件供失败时截取现场。
    """
    lines = []
    with tempfile.TemporaryFile(mode="w+", encoding="utf-8", errors="replace") as sink:
        proc = subprocess.Popen(
            cmd, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True, encoding="utf-8", errors="replace", bufsize=1,
            shell=isinstance(cmd, str),
        )
        started = time.time()
        assert proc.stdout is not None
        for raw in proc.stdout:
            sys.stdout.write("    | " + raw)
            sys.stdout.flush()
            lines.append(raw)
            sink.write(raw)
        rc = proc.wait()          # ← 真实退出码，不经任何管道
    print("    | [%s] 退出码=%d 耗时=%.1fs" % (label, rc, time.time() - started))
    return rc, "".join(lines)


def tail_of(text, n=TAIL_LINES):
    return "\n".join(text.splitlines()[-n:])


def emit(status, text):
    print("  [%s] %s" % (status, text))


# ----------------------------------------------------------------- 后端
def run_backend(packages):
    """后端：go test -count=1。

    包取舍（2026-10-09 实测，勿凭猜测改）：
      `go test -count=1 ./...` 在本机**退出码 0**。`go list ./...` 共 55 个包，其中
      **34 个包有测试且全部 ok**，另 21 个包是 `[no test files]`（无测试文件，不是失败）。
      因此**全量纳入，不排除任何包**。
      用例级明细（-v 实测）：1346 个用例执行（=== RUN 计数），811 个顶层 PASS，
      **0 个 FAIL**，24 处 t.Skip（环境依赖：ffmpeg 二进制 7 / live 7 / index 4 /
      transcode 2 / compute 2 等；既有设计，非本次引入）。
      门禁对 skip 只报 WARN 不阻断——阻断会让门禁在缺 ffmpeg 的机器上永远红，
      反而没人再用它；但 skip 是假绿的温床，新增 skip 必须在评审里说明。
    """
    go = find_tool("go")
    if not go:
        emit("FAIL", "找不到 go 可执行文件 —— 无法执行后端测试（这是阻断项，不是跳过）")
        print("         补救：安装 Go 1.26+并加入 PATH；便携版可放在"
              "~/.workbuddy/binaries/go/versions/<ver>/go.exe")
        return False, ""

    cmd = [go, "test", "-count=1"] + list(packages)
    print("  [INFO] 命令: %s（工作目录 %s）" % (" ".join(cmd), BACKEND))
    print("  [INFO] -count=1 禁构建缓存：否则可能拿到上一次的绿灯")
    rc, out = run_streaming(cmd, BACKEND, "go test")

    if rc != 0:
        emit("FAIL", "后端测试失败（go test 退出码=%d）——阻断发版" % rc)
        print("  ---- 失败现场（末 %d 行）----" % TAIL_LINES)
        for l in tail_of(out).splitlines():
            print("  " + l)
        print("  ---- 失败现场结束 ----")
        return False, out

    # 统计：ok 包数 / skip 数（skip 只报不阻断，理由见 run_backend docstring）
    # ⚠️ 诚实说明：`go test` 不加 -v 时**不会打印 --- SKIP 行**，所以正常情况下
    # skip_hits 恒为 0、WARN 不出现。这不是检测失效，而是非 verbose 模式拿不到该信息。
    # 保留这段是为了：一旦有人给命令加了 -v 或工具链输出了 SKIP 行，WARN 会自动出现。
    # 要真正审计 skip 数量，用：go test -count=1 -v ./... | grep -c '^\s*--- SKIP'
    ok_pkgs = out.count("\nok  ") + (1 if out.startswith("ok  ") else 0)
    no_test = out.count("[no test files]")
    skip_hits = sum(1 for l in out.splitlines() if l.strip().startswith("--- SKIP"))
    emit("PASS", "后端测试全绿（go test -count=1 退出码=0；ok 包 %d 个，"
                  "无测试文件包 %d 个）" % (ok_pkgs, no_test))
    if skip_hits:
        emit("WARN", "检测到 %d 处 t.Skip（环境依赖，既有用法；本次不判失败，"
                     "但 skip 是假绿温床，新增 skip 必须在评审里说明）" % skip_hits)
    return True, out


# ----------------------------------------------------------------- 前端
def run_frontend(install_deps=False):
    """前端：npm run test（= vitest run，单次非watch）。

    - node_modules 缺失时默认 FAIL 并给出补救命令；加 --install-deps 才自动 npm ci
      （npm ci 要联网且耗时数分钟，不适合默认动作）。
    """
    node = find_tool("node")
    npm = find_tool("npm")
    if not node or not npm:
        emit("FAIL", "找不到 node/npm 可执行文件 —— 无法执行前端测试（这是阻断项，不是跳过）")
        print("         补救：安装 Node 22+ 并加入 PATH")
        return False, ""

    if not os.path.isdir(os.path.join(FRONTEND, "node_modules")):
        if not install_deps:
            emit("FAIL", "src/frontend/node_modules 不存在 —— vitest 跑不起来")
            print("         补救一：python scripts/release_test_gate.py --install-deps")
            print("         补救二：cd src/frontend && npm ci --no-audit --no-fund")
            return False, ""
        print("  [INFO] node_modules 缺失，按 --install-deps 先执行 npm ci ...")
        rc, _ = run_streaming([npm, "ci", "--no-audit", "--no-fund"], FRONTEND, "npm ci")
        if rc != 0:
            emit("FAIL", "npm ci 失败（退出码=%d）" % rc)
            return False, ""

    cmd = [npm, "run", "test"]
    print("  [INFO] 命令: npm run test（= vitest run；工作目录 %s）" % FRONTEND)
    rc, out = run_streaming(cmd, FRONTEND, "vitest")
    if rc != 0:
        emit("FAIL", "前端测试失败（vitest 退出码=%d）——阻断发版" % rc)
        print("  ---- 失败现场（末 %d 行）----" % TAIL_LINES)
        for l in tail_of(out).splitlines():
            print("  " + l)
        print("  ---- 失败现场结束 ----")
        return False, out

    emit("PASS", "前端测试全绿（vitest 退出码=0）")
    for l in out.splitlines():
        s = l.strip()
        if s.startswith("Test Files") or s.startswith("Tests"):
            emit("INFO", s)
    return True, out


# ----------------------------------------------------------------- main
def main() -> int:
    ap = argparse.ArgumentParser(
        description="发版测试门禁：后端 go test -count=1 + 前端 vitest，红则阻断发版")
    ap.add_argument("--backend-only", action="store_true")
    ap.add_argument("--frontend-only", action="store_true")
    ap.add_argument("--backend-packages", default="",
                    help="覆盖后端包列表，默认 ./...（逗号分隔）")
    ap.add_argument("--install-deps", action="store_true",
                    help="前端缺 node_modules 时自动 npm ci（需联网，耗时数分钟）")
    args = ap.parse_args()
    if args.backend_only and args.frontend_only:
        print("[gate] --backend-only 与 --frontend-only 不能同时使用")
        return 2

    pkgs = ([p.strip() for p in args.backend_packages.split(",") if p.strip()]
            or BACKEND_PACKAGES)

    print("=== 发版测试门禁（后端 go test -count=1 + 前端 vitest）===")
    print("  [INFO] 仓库根: %s" % ROOT)
    print("  [INFO] 后端包: %s" % ", ".join(pkgs))
    print("  [INFO] 工具链: go=%s" % (find_tool("go") or "缺失"))
    print("  [INFO] 工具链: node=%s" % (find_tool("node") or "缺失"))

    results = []
    if not args.frontend_only:
        print("\n--- 后端测试 ---")
        results.append(("后端 go test -count=1",) + run_backend(pkgs)[:1])
    if not args.backend_only:
        print("\n--- 前端测试 ---")
        results.append(("前端 vitest",) + run_frontend(args.install_deps)[:1])

    print("\n--- 汇总 ---")
    all_ok = True
    for name, ok in results:
        print("  %s %s" % ("RESULT: PASS" if ok else "RESULT: FAILED", name))
        all_ok = all_ok and ok

    print("\n=== 总判定: %s ===" % ("PASS —— 允许继续发版" if all_ok
                                else "FAILED —— 测试红，阻断发版"))
    if not all_ok:
        print("提示：修实现，不要改测试。删断言 / 加 skip 让门禁变绿属于作弊，")
        print("      按《测试完整性反作弊规范》属高危项，须独立复核。")
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())