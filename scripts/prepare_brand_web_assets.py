#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Panomint 品牌资产生成 —— 产出 src/frontend/public/brand/ 下 10 个文件（Job000144）。

参数全部照抄 文档/Spec_时间轴三档维度与日期跳转与品牌接入_v1.0.md §8.2 与
文档/设计规格_增量_维度切换与日期选择与Logo落位_v1.0.md §7.1-7.5，不做任何自由发挥。

两个**实测得出**的工程要点（不照抄伪码，因为照抄会做出坏图）：

1. 合成底色必须用 alpha mask 粘贴。
   源图 `panomint-logo-1024-transparent.png` 的 alpha=0 像素**仍保留原始 RGB**，
   实测值为 rgb(0..9, 0..5, 0..6)（近黑），不是近白。若按设计文档 §7.4 伪码第 6 步
   直接 `paste(mark, (offX, offY))` 不带 mask，这批近黑值会被原样盖到 #e9e4de 底上，
   在标记四周印出一圈黑框。实测：矩形内近黑(<40)像素 29920 个 / 平均亮度 107.3；
   带 mask 粘贴则只有 14 个 / 197.0（底色本身是 228.8）。

2. 透明资产（7-10 号）缩放前必须做**预乘 alpha**，否则 LANCZOS 会把透明区的
   近黑 RGB 混进边缘，产生暗边晕。纯黑残留在 #f4f1ed / #e9e4de 浅底上≈黑描边。

用法：
  python scripts/prepare_brand_web_assets.py
  python scripts/prepare_brand_web_assets.py --outdir <自定义输出目录>
"""

from __future__ import annotations

import argparse
import math
import os
import re
import sys

import numpy as np
from PIL import Image

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
DEFAULT_SRC = os.path.join(ROOT, "assets", "brand", "logo", "panomint-logo-1024-transparent.png")
DEFAULT_OUT = os.path.join(ROOT, "src", "frontend", "public", "brand")

# 合成底色 = var(--color-bg)。PWA/平台类图标不支持透明，必须合成不透明底。
BASE_COLOR = (0xE9, 0xE4, 0xDE)

# 两套裁切定义（源 1024 坐标系）—— Spec §7.2，语义为 (x, y, w, h) 左上角 + 宽高。
#
# 陷阱：PIL 的 Image.crop() 四元组语义是 (left, top, right, bottom)，**不是** (x, y, w, h)。
# 直接把 (x, y, w, h) 传进去会静默裁出错误区域且不报错 —— 实测传 (295,251,411,410)
# 得到的是 116x159 的图（= 411-295, 410-251），整张盘面被裁掉一大半还不报异常。
# 故此处显式转换，任何调用点都不得直接把这四个数塞进 crop()。
CROP_FULL = (169, 251, 663, 551)   # 全标记版，含字标，aspect 1.203
CROP_DISC = (295, 251, 411, 410)   # 盘面版，无字标，aspect 1.002


def pil_box(crop):
    """把 Spec 的 (x, y, w, h) 转成 PIL crop 需要的 (left, top, right, bottom)。"""
    x, y, w, h = crop
    return (x, y, x + w, y + h)

# 不透明平台类图标（1-6 号）：(文件名, 画布边长, 标记宽, 标记高, 偏移 x, 偏移 y, PNG 格式)
# 数值照抄 Spec §8.2 表 + 设计规格 §7.4 数值自检表。
OPAQUE_ICONS = [
    ("favicon-16.png",        16,  14,  14,   1,   1, "PNG8"),
    ("favicon-32.png",        32,  28,  28,   2,   2, "PNG8"),
    ("favicon-48.png",        48,  42,  42,   3,   3, "PNG8"),
    ("apple-touch-icon-180.png", 180, 97, 97, 42,  42, "PNG24"),
    ("maskable-192.png",     192, 104, 104,  44,  44, "PNG24"),
    ("maskable-512.png",     512, 276, 275, 118, 119, "PNG24"),
]

# 透明 UI 类资产（7-10 号）：(文件名, 标记宽, 标记高, 用哪套裁切)
#
# 尺寸口径 = **文件名承诺的标称尺寸**（Job000144 返工）。
# 原实现按裁切框原始像素出图（logo-full-256 实际 663x551），导致文件名说谎：
#   - manifest 只能被迫写 "sizes": "663x551"，后续维护者会以为「256」是像素数；
#   - 一个 72px 的显示位要下载 482KB，全品牌资产的一半以上被单个文件吃掉。
# 现按标称宽等比缩放：全标记版 663:551 → 256x213 / 128x106；
# 盘面版 411:410 aspect 1.0024，按裁定取正方形 64x64 / 32x32（形变 0.24%，不可见）。
# 只缩放不裁剪，盘面保持完整圆形不切边；透明底保留（故角 alpha 必须为 0）。
TRANSPARENT_ICONS = [
    ("logo-full-256.png", 256, 213, CROP_FULL),
    ("logo-full-128.png", 128, 106, CROP_FULL),
    ("logo-disc-64.png",   64,  64, CROP_DISC),
    ("logo-disc-32.png",   32,  32, CROP_DISC),
]


def half_up(v: float) -> int:
    """四舍五入取整。Python 内置 round 是银行家舍入（round(118.5)==118），
    与设计规格 §7.4 数值自检表（maskable-512 的 offY=119）对不上，故自实现。"""
    return int(math.floor(v + 0.5))


def resize_premultiplied(im: Image.Image, size) -> Image.Image:
    """预乘 alpha 缩放，避免透明区残留 RGB 渗进边缘产生暗边晕。"""
    a = np.asarray(im).astype(np.float64)
    alpha = a[:, :, 3:4] / 255.0
    a[:, :, :3] *= alpha                       # 预乘
    pm = Image.fromarray(np.clip(a, 0, 255).astype(np.uint8), "RGBA")
    pm = pm.resize(size, Image.LANCZOS)
    b = np.asarray(pm).astype(np.float64)
    a2 = np.clip(b[:, :, 3:4] / 255.0, 1e-6, None)
    b[:, :, :3] = np.clip(b[:, :, :3] / a2, 0, 255)   # 反预乘
    out = b.astype(np.uint8)
    # alpha=0 处的 RGB 按 PNG 规范属未定义值，但部分浏览器做双线性插值时会把它
    # 混进邻近半透明像素，形成一圈亮边。这里显式清零，让边缘只剩 alpha 决定形状。
    out[out[:, :, 3] == 0, :3] = 0
    return Image.fromarray(out, "RGBA")


def build_opaque(src_rgb: Image.Image, crop, canvas, mw, mh, ox, oy, fmt):
    """平台类图标：盘面版 + #e9e4de 纯色底 + 圆角 0。

    先在原生分辨率合成再缩放（而非先缩放再合成），从根上避免透明区暗边晕：
    合成后画面里根本没有透明像素，LANCZOS 无从掺入黑色。
    """
    mark_native = src_rgb.crop(pil_box(crop))     # 411x410，无透明
    assert mark_native.size == (crop[2], crop[3]), \
        "裁切尺寸不符：得到 %s，期望 %s" % (mark_native.size, (crop[2], crop[3]))
    flat = Image.new("RGB", mark_native.size, BASE_COLOR)
    flat.paste(mark_native, (0, 0))
    flat = flat.resize((mw, mh), Image.LANCZOS)
    out = Image.new("RGB", (canvas, canvas), BASE_COLOR)
    out.paste(flat, (ox, oy))
    return out


def build_transparent(src_rgba: Image.Image, crop, mw, mh):
    """UI 类资产：保持透明底，画布 = 标记尺寸。"""
    mark = src_rgba.crop(pil_box(crop))
    assert mark.size == (crop[2], crop[3]), \
        "裁切尺寸不符：得到 %s，期望 %s" % (mark.size, (crop[2], crop[3]))
    if mark.size == (mw, mh):
        # 无需缩放，但源图 alpha=0 处仍带原始 RGB（实测近黑），同样清零
        arr = np.array(mark)
        arr[arr[:, :, 3] == 0, :3] = 0
        return Image.fromarray(arr, "RGBA")
    return resize_premultiplied(mark, (mw, mh))


def save(img: Image.Image, path: str, fmt: str):
    if fmt == "PNG8":
        img.convert("P", palette=Image.ADAPTIVE, colors=256).save(path, "PNG", optimize=True)
    elif fmt == "PNG24":
        img.save(path, "PNG", optimize=True)          # RGB 模式 = 无 alpha 通道
    else:
        img.save(path, "PNG", optimize=True)          # RGBA = 真透明


def inspect(path: str):
    """自检：真实读回文件，报告 mode / size / 非透明像素数。"""
    im = Image.open(path)
    a = np.asarray(im.convert("RGBA"))
    alpha = a[:, :, 3]
    opaque_px = int((alpha == 255).sum())
    clear_px = int((alpha == 0).sum())
    has_a = "A" in im.getbands()
    return {
        "mode": im.mode,
        "size": im.size,
        "has_alpha": has_a,
        "alpha_min": int(alpha.min()),
        "opaque": opaque_px,
        "clear": clear_px,
        "total": alpha.size,
        "bytes": os.path.getsize(path),
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", default=DEFAULT_SRC)
    ap.add_argument("--outdir", default=DEFAULT_OUT)
    args = ap.parse_args()

    src_rgba = Image.open(args.src).convert("RGBA")
    # 合成用 RGB 底：alpha=0 处 RGB 为近黑，必须先把透明区刷成底色再 paste，
    # 否则无 mask 粘贴会把近黑盖到画布上（见模块 docstring 要点 1）。
    src_rgb = Image.new("RGB", src_rgba.size, BASE_COLOR)
    src_rgb.paste(src_rgba, (0, 0), src_rgba)

    os.makedirs(args.outdir, exist_ok=True)
    print("源图: %s" % os.path.relpath(args.src, ROOT))
    print("输出: %s" % os.path.relpath(args.outdir, ROOT))
    print("底色: #e9e4de   圆角: 0（maskable 由系统二次裁切）\n")

    made = []
    for name, canvas, mw, mh, ox, oy, fmt in OPAQUE_ICONS:
        # 落位自检（容差 1px）。Spec §7.4 自检表给的偏移是**亚像素取整**结果：
        # 180 画布 97 宽 → (180-97)/2 = 41.5，锁定值取 42，左 42 / 右 41；
        # 512 画布 275 高 → (512-275)/2 = 118.5，锁定值取 119，上 119 / 下 118。
        # 这半像素偏移是 Spec 锁定值，不重算；但必须守住两条硬约束：
        #   1) 标记完整落在画布内（不裁切）
        #   2) 偏移与几何中心相差 ≤ 1px（不偏心）
        assert ox >= 0 and oy >= 0 and ox + mw <= canvas and oy + mh <= canvas, \
            "%s 标记溢出画布" % name
        assert abs((canvas - mw) / 2 - ox) <= 1 and abs((canvas - mh) / 2 - oy) <= 1, \
            "%s 偏移偏离几何中心超过 1px" % name
        img = build_opaque(src_rgb, CROP_DISC, canvas, mw, mh, ox, oy, fmt)
        p = os.path.join(args.outdir, name)
        save(img, p, fmt)
        made.append((name, p, fmt))

    for name, mw, mh, crop in TRANSPARENT_ICONS:
        img = build_transparent(src_rgba, crop, mw, mh)
        p = os.path.join(args.outdir, name)
        save(img, p, "PNG32")
        made.append((name, p, "PNG32"))

    print("%-24s %-6s %-12s %-9s %8s %10s %9s %8s" % (
        "文件", "mode", "size", "有alpha通道", "不透明px", "全透明px", "体积B", "预期"))
    print("-" * 104)
    ok = True
    for name, p, fmt in made:
        s = inspect(p)
        want_alpha = fmt == "PNG32"
        if want_alpha:
            # 透明资产：必须有 alpha 通道，且真的存在全透明像素（否则等于没透明）
            good = s["has_alpha"] and s["clear"] > 0
        else:
            # 不透明资产：无 alpha 通道，且每一个像素都不透明
            good = (not s["has_alpha"]) and s["opaque"] == s["total"]
        ok = ok and good
        print("%-24s %-6s %-12s %-9s %8d %10d %9d %8s" % (
            name, s["mode"], "%dx%d" % s["size"], "是" if s["has_alpha"] else "否",
            s["opaque"], s["clear"], s["bytes"], "PASS" if good else "FAIL"))

    # 落位几何自检：核对不透明资产里「非底色内容框」是否等于锁定的标记尺寸与偏移。
    # 这条能挡住「裁切框传错导致整图裁掉一大半」这类不报错的静默失败。
    print("\n落位几何自检（非底色像素框 vs 锁定值）:")
    print("%-24s %-14s %-14s %-8s" % ("文件", "实际内容框", "锁定 宽x高@偏移", "判定"))
    base_np = np.array(BASE_COLOR)
    for name, canvas, mw, mh, ox, oy, fmt in OPAQUE_ICONS:
        arr = np.asarray(Image.open(os.path.join(args.outdir, name)).convert("RGB")).astype(int)
        nb = np.abs(arr - base_np).max(axis=2) > 6
        ys, xs = np.nonzero(nb)
        got = "%dx%d@(%d,%d)" % (xs.max()-xs.min()+1, ys.max()-ys.min()+1, xs.min(), ys.min())
        want = "%dx%d@(%d,%d)" % (mw, mh, ox, oy)
        # 内容框可能因抗锯齿边缘极浅而比锁定框小 1-2px，只断言不超出锁定框
        fits = (xs.min() >= ox-2 and ys.min() >= oy-2 and
                xs.max() <= ox+mw+1 and ys.max() <= oy+mh+1 and
                (xs.max()-xs.min()+1) >= mw-3 and (ys.max()-ys.min()+1) >= mh-3)
        ok = ok and fits
        print("%-24s %-14s %-14s %-8s" % (name, got, want, "PASS" if fits else "FAIL"))

    # 文件名承诺自检（Job000144 返工核心）：文件名里的标称数字必须等于实际像素尺寸。
    # 这条挡住「文件名说谎」——原始缺陷正是 logo-full-256.png 实际为 663x551，
    # 而文件名与 manifest 都对外承诺 256，体积还白白吃掉 482KB。
    print("\n文件名承诺自检（文件名标称数字 vs 实际像素）:")
    print("%-24s %-12s %-14s %-10s %8s" % ("文件", "实际尺寸", "文件名承诺", "判定", "体积B"))
    for name, p, fmt in made:
        s = inspect(p)
        digits = re.findall(r"(\d+)", name)
        if not digits:
            continue
        # 承诺值 = 文件名里唯一的标称数字（favicon-16 / maskable-512 / logo-full-256…）
        claim = int(digits[-1])
        # 平台类资产画布是正方形（宽==高==标称）；UI 类按宽或高命中标称即可
        # （logo-full-256 实际 256x213，宽命中；logo-disc-64 为 64x64 两侧都命中）。
        match = (claim in s["size"])
        ok = ok and match
        print("%-24s %-12s %-14s %-10s %8d" % (
            name, "%dx%d" % s["size"], str(claim),
            "PASS" if match else "FAIL", s["bytes"]))

    # 透明资产必须真有透明像素。
    # 注意：**不能**断言「四角alpha==0」——盘面版（logo-disc-*）的 artwork 本身就
    # 铺满裁切框（实测源裁切框四边余量均为 0），故其角落是实心画面而非透明留白，
    # 这是原图特性而非切边缺陷。全标记版（logo-full-*）才是四角全透明。
    print("\n透明资产透明层自检:")
    print("%-24s %-6s %-12s %-10s %-12s %8s" % (
        "文件", "mode", "尺寸", "有alpha通道", "全透明px", "判定"))
    for name, p, fmt in made:
        if fmt != "PNG32":
            continue
        s = inspect(p)
        good = s["has_alpha"] and s["clear"] > 0
        ok = ok and good
        print("%-24s %-6s %-12s %-10s %-12d %8s" % (
            name, s["mode"], "%dx%d" % s["size"], "是" if s["has_alpha"] else "否",
            s["clear"], "PASS" if good else "FAIL"))
    print("  注：盘面版四角为实心画面（源 artwork 铺满裁切框），非缺陷；")
    print("      全标记版四角全透明。二者均为 RGBA 且含真透明像素。")

    print("\nRESULT: %s  共 %d 个文件" % ("OK" if ok else "FAIL", len(made)))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())