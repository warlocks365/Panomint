#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Panomint 正式 Logo 资产准备 —— 去除 AI 生成标识。

输入：AI 生成的 1024x1024 渲染图（RGB，无透明通道；「棋盘格」是烤进像素的
      近白底纹，不是真透明，所以无法靠裁切+补透明边解决）。
做法：定位标识像素 → 从**同高度的水平镜像干净区**克隆覆盖 → 边缘羽化混合。
      不用「重画棋盘格」，因为底纹是带压缩噪点的柔和方格（242~255），
      无干净可复制的图案，重画会露出补丁痕迹。

输出：assets/brand/logo/ 下的标准尺寸资产 + 处理报告。

用法：
  python scripts/prepare_logo_asset.py --src "<源图路径>"
  python scripts/prepare_logo_asset.py --src "<源图>" --transparent   # 额外出透明底版
"""

from __future__ import annotations

import argparse
import os
import sys
from collections import deque

from PIL import Image, ImageFilter

# 标识像素判定：比底纹明显暗。底纹最暗约 242，标识笔画约 200~225。
WATERMARK_LUM = 232
# 判定「底纹」的阈值：足够亮且接近中性
BG_MIN_CH = 236
BG_MAX_SPREAD = 10
# 判定范围先按右下角限定（避免误伤图形本体）
SEARCH_BOX = (0.5, 0.90, 1.0, 1.0)     # (x0, y0, x1, y1) 相对比例
FEATHER = 7                             # 边缘羽化半径（像素）


def lum(r, g, b):
    return 0.299 * r + 0.587 * g + 0.114 * b


def find_watermark_bbox(im: Image.Image):
    """在限定区域内找出标识的精确包围盒"""
    W, H = im.size
    px = im.load()
    x0, y0 = int(W * SEARCH_BOX[0]), int(H * SEARCH_BOX[1])
    x1, y1 = int(W * SEARCH_BOX[2]), int(H * SEARCH_BOX[3])
    minx, miny, maxx, maxy, cnt = W, H, -1, -1, 0
    for y in range(y0, y1):
        for x in range(x0, x1):
            r, g, b = px[x, y]
            if lum(r, g, b) < WATERMARK_LUM:
                cnt += 1
                if x < minx: minx = x
                if x > maxx: maxx = x
                if y < miny: miny = y
                if y > maxy: maxy = y
    if cnt == 0:
        return None
    return minx, miny, maxx, maxy, cnt


def count_dark(im: Image.Image, box) -> int:
    x0, y0, x1, y1 = box
    px = im.load()
    n = 0
    for y in range(y0, y1 + 1):
        for x in range(x0, x1 + 1):
            if lum(*px[x, y]) < WATERMARK_LUM:
                n += 1
    return n


def clone_mirror(im: Image.Image, box, feather=FEATHER):
    """用同高度、水平镜像位置的干净区域覆盖标识区（羽化混合）"""
    W, H = im.size
    x0, y0, x1, y1 = box
    m = feather + 2
    x0, y0 = max(0, x0 - m), max(0, y0 - m)
    x1, y1 = min(W - 1, x1 + m), min(H - 1, y1 + m)
    w, h = x1 - x0 + 1, y1 - y0 + 1

    # 源：同 y，x 镜像
    src = Image.new("RGB", (w, h))
    sp, dp = im.load(), src.load()
    for dy in range(h):
        sy = y0 + dy
        for dx in range(w):
            sx = W - 1 - (x0 + dx)          # 水平镜像
            if 0 <= sx < W:
                dp[dx, dy] = sp[sx, sy]

    # 羽化遮罩：中心实、两端渐隐
    mask = Image.new("L", (w, h), 0)
    inset = feather
    ImageDraw_rect = mask.load()
    for dy in range(h):
        for dx in range(w):
            ex = min(dx, w - 1 - dx)
            ey = min(dy, h - 1 - dy)
            e = min(ex, ey)
            v = 255 if e >= inset else int(255 * max(e, 0) / max(inset, 1))
            ImageDraw_rect[dx, dy] = v
    mask = mask.filter(ImageFilter.GaussianBlur(1.2))

    out = im.copy()
    out.paste(src, (x0, y0), mask)
    return out, (x0, y0, x1, y1)


def make_transparent(im: Image.Image, min_enclosed: int = 1200):
    """把底纹像素置为透明，分两轮，兼顾「不误伤」与「不留斑」。

    轮 1 边缘洪泛：只染与四边连通的底纹 —— 绝对安全，但胶片带把中间那圈
           背景**完全围住**，洪泛到不了，深色底上会留一片亮斑。
    轮 2 面积阈值：残留的底纹连通域中，面积 >= min_enclosed 的判为背景并补透明。
           图形内部的白色高光（字母内腔、描边）面积都远小于该阈值，故不会被补。

    阈值是权衡：调大会留斑，调小会打穿高光。默认 1200 px 经实测可兼顾。
    """
    W, H = im.size
    sp = im.load()                       # PixelAccess，不能 .copy()，只能按点读

    def is_bg(x, y):
        r, g, b = sp[x, y]
        if min(r, g, b) < BG_MIN_CH:
            return False
        if max(r, g, b) - min(r, g, b) > BG_MAX_SPREAD:
            return False
        return True

    seen = bytearray(W * H)
    q = deque()
    filled = []                          # 记录已置透明的坐标，避免末尾再全图扫一遍

    def push(x, y):
        if not seen[y * W + x] and is_bg(x, y):
            seen[y * W + x] = 1
            q.append((x, y))

    for x in range(W):
        push(x, 0)
        push(x, H - 1)
    for y in range(H):
        push(0, y)
        push(W - 1, y)

    while q:                             # 轮 1：边缘洪泛
        x, y = q.popleft()
        filled.append((x, y))
        for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)):
            nx, ny = x + dx, y + dy
            if 0 <= nx < W and 0 <= ny < H:
                push(nx, ny)
    n_edge = len(filled)

    # 轮 2：残留连通域按面积筛选
    rest = bytearray(W * H)
    for y in range(H):
        for x in range(W):
            if not seen[y * W + x] and is_bg(x, y):
                rest[y * W + x] = 1
    n_enclosed = 0
    n_kept = 0
    for y in range(H):
        for x in range(W):
            if not rest[y * W + x]:
                continue
            comp = []
            dq = deque([(x, y)])
            rest[y * W + x] = 0
            while dq:
                cx, cy = dq.popleft()
                comp.append((cx, cy))
                for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                    nx, ny = cx + dx, cy + dy
                    if 0 <= nx < W and 0 <= ny < H and rest[ny * W + nx]:
                        rest[ny * W + nx] = 0
                        dq.append((nx, ny))
            if len(comp) >= min_enclosed:
                filled.extend(comp)
                n_enclosed += len(comp)
            else:
                n_kept += len(comp)      # 判定为图形内高光，保留不透明

    out = im.convert("RGBA")
    op = out.load()
    for x, y in filled:
        r, g, b = sp[x, y]
        op[x, y] = (r, g, b, 0)
    return out, n_edge, n_enclosed, n_kept


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", required=True)
    ap.add_argument("--outdir", default="")
    ap.add_argument("--transparent", action="store_true")
    ap.add_argument("--min-enclosed", type=int, default=1200,
                    help="被围住的底纹连通域面积阈值：>= 该值判为背景补透明，"
                         "更小则判为图形内高光保留。默认 1200")
    args = ap.parse_args()

    root = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    outdir = args.outdir or os.path.join(root, "assets", "brand", "logo")
    os.makedirs(outdir, exist_ok=True)

    im = Image.open(args.src).convert("RGB")
    W, H = im.size
    print("源图: %dx%d  mode=%s" % (W, H, Image.open(args.src).mode))

    box = find_watermark_bbox(im)
    if not box:
        print("RESULT: 未检出标识像素，无需处理")
        return 0
    bx0, by0, bx1, by1, cnt = box
    print("检出标识: 包围盒 x=%d..%d y=%d..%d，暗像素 %d 个" % (bx0, bx1, by0, by1, cnt))
    print("处理前 该区域暗像素计数 = %d" % count_dark(im, (bx0, by0, bx1, by1)))

    fixed, patched = clone_mirror(im, (bx0, by0, bx1, by1))
    after = count_dark(fixed, (bx0, by0, bx1, by1))
    print("处理后 该区域暗像素计数 = %d" % after)

    p_master = os.path.join(outdir, "panomint-logo-1024.png")
    fixed.save(p_master)
    print("主资产: %s (%d B)" % (os.path.relpath(p_master, root), os.path.getsize(p_master)))

    for s in (512, 256, 128, 64, 32, 16):
        p = os.path.join(outdir, "panomint-logo-%d.png" % s)
        fixed.resize((s, s), Image.LANCZOS).save(p)
        print("  尺寸 %3d -> %s (%d B)" % (s, os.path.relpath(p, root), os.path.getsize(p)))

    if args.transparent:
        tr, n_edge, n_enc, n_kept = make_transparent(fixed, args.min_enclosed)
        p = os.path.join(outdir, "panomint-logo-1024-transparent.png")
        tr.save(p)
        print("透明底版: %s" % os.path.relpath(p, root))
        print("  轮1 边缘洪泛 %d px (%.2f%%)"
              % (n_edge, 100.0 * n_edge / (W * H)))
        print("  轮2 围住背景 %d px (%.2f%%)  阈值 %d"
              % (n_enc, 100.0 * n_enc / (W * H), args.min_enclosed))
        print("  保留为高光   %d px (%.2f%%)" % (n_kept, 100.0 * n_kept / (W * H)))
        print("  转透明合计   %d px (%.2f%%)"
              % (n_edge + n_enc, 100.0 * (n_edge + n_enc) / (W * H)))
        for s in (512, 256, 128, 64, 32, 16):
            p = os.path.join(outdir, "panomint-logo-%d-transparent.png" % s)
            tr.resize((s, s), Image.LANCZOS).save(p)

    print("RESULT: OK  残留暗像素 %d（%s）"
          % (after, "干净" if after <= 2 else "仍有残留，需扩大克隆区"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
