#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""构建输入指纹：判定「运行中的镜像是否已跟上源码」。

背景（2026-10-09 Job000145 之后的第②项短板）
  失败形态 B 是「源码更新了，但镜像没重建，线上跑的是旧代码」。
  这类漂移**不会自己发出任何报错** —— 容器健康、/ready 全绿、日志干净，
  唯一的线索是「磁盘上的源码」比「镜像里的产物」新。
  所以必须有一个独立于运行时的比对手段，指纹就是那个手段。

算法（刻意做成「只认 Dockerfile 真正吃进去的东西」）
  1. 解析目标 Dockerfile 的 COPY 指令，只取「把构建上下文里的内容拷进镜像」的源路径；
     跳过 `--from=`（阶段间拷贝，产物由前面的 RUN 生成，与源码无关）
     与 `COPY <<EOF`（内联配置，根本不是文件）。
     为什么要抠到 COPY 级别，而不是直接哈希整个 context 目录：
       api 的 build context 是**仓库根**，里面还有 node_modules、截图、备份包、
       .workbuddy 临时文件等与镜像无关的东西。它们随便动一下就误报，
       误报几次之后就没人再信这个告警了 —— 那等于没有检查。
  2. 每个源文件取摘要：
       - <= 2MiB：算内容 sha256（源码改动必然改变它）
       - >  2MiB：只取 (size, mtime_ns)
     为什么大文件不哈希内容：镜像要吃 581MB 的 CLIP / Chinese-CLIP / 人脸模型，
     全量读一遍会让巡检从毫秒级退化到秒级，每 2 分钟一次纯属浪费；
     而这类文件是 `scripts/fetch-*-assets.sh` 下载来的，本来就不会被手改，
     size + mtime 对它们足够敏感。
  3. 所有 (相对路径, 摘要) **排序后**再 sha256。
     排序必须有：否则目录遍历顺序一变（新增文件、inode 重排）就会伪装成源码变化。

指纹还额外吃进 compose 解析出的 build args：
  因为 build arg 变了（APP_VERSION / APP_COMMIT / ORT_VERSION 等）镜像内容就会变，
  而 Dockerfile 的 COPY 列表可能一个字都没动。
  反过来，运行时环境变量（CORS_ORIGINS 之类）不在 build args 里，改它们不会误报。

用法
  python3 ops_fingerprint.py --context <build 上下文目录> --dockerfile <相对或绝对路径> \\
                             [--build-args "<解析出的 build args>"] [--project <项目名>] \\
                             [--extra-input <路径>]...

  --extra-input 的必要性（api 镜像实测踩到的坑）：
    docker/api/Dockerfile 的 ortassets 阶段用
        RUN --mount=type=bind,target=/ctx,ro  need "${ort_dir}/lib/libonnxruntime.so" …
    把构建上下文**只读挂载**进来做存在性/架构断言，真正的字节拷贝发生在同文件后半段的
        COPY --from=ortassets /opt/onnxruntime/lib/libonnxruntime.so* …
    于是 assets/lib/onnxruntime-linux-*/lib/libonnxruntime.so 确实是构建输入，
    但 COPY 解析器看不到它（COPY 那条带 --from=，会被正确跳过）。
    漏掉它的后果很具体：把 .so 换成另一个架构的版本时指纹不变、漂移检测不告警，
    而运行期会 dlopen 失败 —— 正是 Dockerfile 自己花了几十行断言要防的那件事。
    所以这类「经 bind mount / RUN 直接读取」的输入必须由调用方显式补充。

输出 JSON
  {
    "fingerprint": "…",           # 16 进制短指纹
    "file_count":   439,
    "total_bytes":  1234567,
    "newest_mtime": 1791521310,   # 构建输入里最新的 mtime（epoch 秒）
    "missing":      [],           # Dockerfile 要求但磁盘上不存在的输入
    "inputs":       [ … ]         # 按 mtime 倒序的前 8 项，供告警指出「是哪个文件比镜像新」
  }

退出码
  0 指纹算出
  2 参数错误 / Dockerfile 不存在 / context 不存在
  3 构建输入缺失（assets 没拉下来之类）—— 这种情况镜像本来就构建不出来，必须显式失败
"""

import argparse
import glob
import hashlib
import json
import os
import sys

CONTENT_HASH_MAX = 2 * 1024 * 1024   # 超过此大小只取 (size, mtime_ns)
MAX_NEWEST_INPUTS = 8                # 告警里最多列几个「比镜像新」的文件
SKIP_DIRS = {".git", "node_modules", "__pycache__"}


def die(msg, code=2):
    sys.stderr.write(msg.rstrip() + "\n")
    sys.exit(code)


def read_dockerfile(path):
    if not os.path.isfile(path):
        die("Dockerfile 不存在: %s" % path)
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        raw = f.read()
    # 折叠行尾续行：否则一条 COPY 被拆成多行后解析不出来
    return raw.replace("\\\n", " ").splitlines()


def copy_sources(dockerfile_path):
    """取出所有「从构建上下文拷进镜像」的源路径。"""
    out = []
    for line in read_dockerfile(dockerfile_path):
        s = line.strip()
        if not s.startswith("COPY "):
            continue
        if "--from=" in s:
            continue          # 阶段间拷贝：产物来自前面的 RUN，与源码无关
        if "<<" in s:
            continue          # COPY <<'EOF' … 内联配置，不是文件
        parts = s.split()[1:]
        if len(parts) < 2:
            continue          # 至少要 1 个源 + 1 个目标
        for src in parts[:-1]:
            if src.startswith("--"):
                continue      # COPY --chown=… 之类的选项
            out.append(src)
    return out


def digest_file(path):
    st = os.stat(path)
    if st.st_size <= CONTENT_HASH_MAX:
        h = hashlib.sha256()
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(1024 * 1024), b""):
                h.update(chunk)
        return "sha256:" + h.hexdigest(), st
    return "meta:%d:%d" % (st.st_size, st.st_mtime_ns), st


def expand(ctx, src):
    """把一条 COPY 的源展开成文件列表。"""
    abs_src = src if os.path.isabs(src) else os.path.join(ctx, src)
    if any(ch in src for ch in "*?["):
        return sorted(glob.glob(abs_src, recursive=True))
    if os.path.isdir(abs_src):
        acc = []
        for root, dirs, files in os.walk(abs_src):
            dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
            acc.extend(os.path.join(root, name) for name in files)
        return sorted(acc)
    return [abs_src] if os.path.exists(abs_src) else []


def compute(context, dockerfile, build_args, project, extra_inputs=()):
    ctx = os.path.realpath(context)
    if not os.path.isdir(ctx):
        die("构建上下文目录不存在: %s" % ctx)

    df = dockerfile if os.path.isabs(dockerfile) else os.path.join(ctx, dockerfile)
    df = os.path.realpath(df)

    entries = {}   # 绝对路径 -> (摘要, st)
    missing = []
    # Dockerfile 自身**必须**计入指纹：改它一定改变构建结果（加一条 COPY、换基础镜像、
    # 改 ARG 默认值……），而 COPY 解析器只看 COPY 指令，永远看不到「Dockerfile 变了」。
    # 2026-10-09 那次事故的根因就写在 docker/api/Dockerfile 里 —— 如果它不参与指纹，
    # 「改了 Dockerfile 但没重建」这种漂移是查不出来的。
    sources = [df] + copy_sources(df) + list(extra_inputs)
    for src in sources:
        found = expand(ctx, src)
        if not found:
            missing.append(src)
            continue
        for p in found:
            if os.path.isfile(p):
                entries[p] = digest_file(p)

    if not entries:
        die("构建输入为空：COPY 列表没解析出任何文件（Dockerfile=%s）" % df, 3)

    lines = []
    for p in sorted(entries):
        digest, st = entries[p]
        rel = os.path.relpath(p, ctx)
        lines.append("%s\t%s\t%d" % (rel, digest, st.st_size))
    lines.sort()

    h = hashlib.sha256()
    for line in lines:
        h.update(line.encode("utf-8"))
        h.update(b"\n")
    # build args 与项目名一并吃进指纹：它们变了镜像内容就变
    h.update(("build_args=%s\nproject=%s\n" % (build_args, project)).encode("utf-8"))

    newest = 0
    newest_path = ""
    for p, (_digest, st) in entries.items():
        if st.st_mtime > newest:
            newest = st.st_mtime
            newest_path = os.path.relpath(p, ctx)

    top = sorted(entries.items(), key=lambda kv: kv[1][1].st_mtime, reverse=True)
    inputs = [
        {
            "path": os.path.relpath(p, ctx),
            "mtime": int(st.st_mtime),
            "size": st.st_size,
        }
        for p, (_d, st) in top[:MAX_NEWEST_INPUTS]
    ]

    return {
        "fingerprint": h.hexdigest()[:16],
        "file_count": len(entries),
        "total_bytes": sum(st.st_size for _d, st in entries.values()),
        "newest_mtime": int(newest),
        "newest_path": newest_path,
        "dockerfile": os.path.relpath(df, ctx) if df.startswith(ctx) else df,
        "missing": sorted(missing),
        "inputs": inputs,
    }


def main():
    ap = argparse.ArgumentParser(description="计算构建输入指纹")
    ap.add_argument("--context", required=True, help="构建上下文目录（compose 里的 build.context）")
    ap.add_argument("--dockerfile", required=True, help="Dockerfile 路径（相对 context 或绝对）")
    ap.add_argument("--build-args", default="", help="compose 解析出的 build args，原样吃进指纹")
    ap.add_argument("--project", default="", help="compose 项目名，原样吃进指纹")
    ap.add_argument("--extra-input", action="append", default=[],
                    help="COPY 解析不到但确实参与构建的输入（相对 context 或绝对），可重复")
    ap.add_argument("--indent", type=int, default=2)
    a = ap.parse_args()

    result = compute(a.context, a.dockerfile, a.build_args, a.project, a.extra_input)
    print(json.dumps(result, ensure_ascii=False, indent=a.indent))
    # 输入缺失必须以非 0 退出：镜像本来就构建不出来，此时任何「构建成功」的判断都不可信
    sys.exit(3 if result["missing"] else 0)


if __name__ == "__main__":
    main()
