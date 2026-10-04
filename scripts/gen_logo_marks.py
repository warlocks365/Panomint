#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Panomint 产品商标（Logo）方案生成器 —— 10 个不同语义角度。

⚠️ 与 scripts/gen_icon_sheets.py 的区别：
   - gen_icon_sheets.py = 界面内的**功能图标库**（12 语义 × 10 视觉风格）
   - 本脚本          = **产品商标 / 品牌标识**（10 个**不同语义角度**各成一套）
   商标与 UI 皮肤/主题解耦，不随界面配色走。

10 个语义角度（各自独立，不是同一概念的换色变体）：
   01 全景环视  360° 球面回放        → 圆 + 经纬线
   02 品牌字面  Pano + mint 双关     → 有机叶片
   03 成像原理  双鱼眼合成 360        → 双圆交叠
   04 投影数学  等距柱状 2:1         → 枕形条带
   05 画幅比例  超宽全景构图          → 极宽取景框
   06 时间维度  时间轴组织            → 横轴 + 3 节点
   07 空间维度  空间 / VR 漫游       → 等轴测立方
   08 拼接工艺  全景缝合              → 三折竖板
   09 智能维度  AI 语义检索           → 球 + 节点环 + 星芒
   10 部署形态  自托管 / 私有         → 字母 P

输出：文档/商标设计/{sheet_logos,sheet_sizes,logo_NN_*}.png
用法：python scripts/gen_logo_marks.py [--svg-only|--check-only|--only-matrix]
依赖：仅标准库；光栅化调用本机 Chrome headless。
"""

from __future__ import annotations

import argparse
import colorsys
import math
import os
import struct
import subprocess
import sys
from xml.sax.saxutils import escape as xesc

# ---------------------------------------------------------------- 品牌色
# 「晨雾 Morandi」：饱和度封顶、无纯黑纯白。三色分工见 ROLE 注释。
C_MAIN = "#4a5a6a"      # 深雾蓝 —— 轮廓主体
C_ACCENT = "#c4a57a"    # 燕麦金 —— 语义重点
C_SECOND = "#b0766a"    # 陶土红 —— 辅助细节
C_INK = "#41403c"       # 炭灰

ROLE = {"main": C_MAIN, "accent": C_ACCENT, "secondary": C_SECOND}

TH = {"border": "#ddd8d0", "hair": "#e9e4de", "ink": C_INK,
       "text2": "#7d7a73", "text3": "#a5a29a", "title": C_INK}
THD = {"border": "#2c333b", "hair": "#232931", "ink": "#e6ebf0",
       "text2": "#9aa7b4", "text3": "#6f7c89", "title": "#f2f5f8"}

FONT = ('"Outfit","Segoe UI","Noto Sans SC","PingFang SC","Microsoft YaHei",'
        '"DejaVu Sans","Liberation Sans","Helvetica Neue",sans-serif')

# 商标线宽（100 网格）≈ 24 网格下的 1.56，与项目现有图标规范一致
STROKE = 6.5


# ---------------------------------------------------------------- 工具
def _f(v: float) -> str:
    return f"{v:.2f}".rstrip("0").rstrip(".")


def _pts(pairs) -> str:
    return " ".join(f"{_f(x)},{_f(y)}" for x, y in pairs)


def lighten_hex(h: str, amount: float) -> str:
    h = h.lstrip("#")
    r, g, b = (int(h[i:i + 2], 16) / 255.0 for i in (0, 2, 4))
    hh, l, s = colorsys.rgb_to_hls(r, g, b)
    l = min(l + (1.0 - l) * amount, 1.0)
    r2, g2, b2 = colorsys.hls_to_rgb(hh, l, s)
    return "#%02x%02x%02x" % (round(r2 * 255), round(g2 * 255), round(b2 * 255))


def ngon(cx, cy, r, n=6, rot=0.0):
    pts = []
    for k in range(n):
        a = math.radians(rot + 360.0 * k / n)
        pts.append((cx + r * math.cos(a), cy + r * math.sin(a)))
    return pts


def sparkle(cx, cy, ro, ri):
    pts = []
    for k in range(8):
        r = ro if k % 2 == 0 else ri
        a = math.radians(-90 + 45 * k)
        pts.append((cx + r * math.cos(a), cy + r * math.sin(a)))
    return pts


# ---------------------------------------------------------------- 图元
def primitive(kind: str, params):
    if kind == "path":
        return "path", {"d": params}
    if kind == "circle":
        cx, cy, r = params
        return "circle", {"cx": _f(cx), "cy": _f(cy), "r": _f(r)}
    if kind == "ellipse":
        cx, cy, rx, ry = params
        return "ellipse", {"cx": _f(cx), "cy": _f(cy), "rx": _f(rx), "ry": _f(ry)}
    if kind == "ellipse-rot":
        cx, cy, rx, ry, deg = params
        return "ellipse", {
            "cx": _f(cx), "cy": _f(cy), "rx": _f(rx), "ry": _f(ry),
            "transform": f"rotate({_f(deg)} {_f(cx)} {_f(cy)})"}
    if kind == "rect":
        x, y, w, h, rx = params
        return "rect", {"x": _f(x), "y": _f(y), "width": _f(w),
                        "height": _f(h), "rx": _f(rx)}
    if kind == "line":
        x1, y1, x2, y2 = params
        return "line", {"x1": _f(x1), "y1": _f(y1), "x2": _f(x2), "y2": _f(y2)}
    if kind == "polyline":
        return "polyline", {"points": _pts(params)}
    if kind == "polygon":
        return "polygon", {"points": _pts(params)}
    if kind == "dotcircle":
        cx, cy, r = params
        return "circle", {"cx": _f(cx), "cy": _f(cy), "r": _f(r)}
    raise ValueError("unknown primitive: " + kind)


def glyph_elements(mark: dict, mono: bool = False, darken: float = 0.0) -> str:
    """商标图形元素（坐标空间固定 0 0 100 100）"""
    def col_of(role):
        c = C_INK if mono else ROLE[role]
        return lighten_hex(c, darken) if darken else c

    out = []
    for kind, params, role in mark["parts"]:
        col = col_of(role)
        tag, attrs = primitive(kind, params)
        a = " ".join(f'{k}="{v}"' for k, v in attrs.items())
        if kind == "dotcircle":
            out.append(f'<circle {a} fill="{col}"/>')
        else:
            out.append(f'<{tag} {a} fill="none" stroke="{col}" '
                       f'stroke-width="{STROKE}" stroke-linecap="round" '
                       f'stroke-linejoin="round"/>')
    return "".join(out)


def embed(mark, x, y, size, mono=False, darken=0.0):
    return (f'<svg x="{_f(x)}" y="{_f(y)}" width="{_f(size)}" '
            f'height="{_f(size)}" viewBox="0 0 100 100" fill="none">'
            f"{glyph_elements(mark, mono, darken)}</svg>")


def txt(x, y, s, size=13, weight=400, fill=C_INK, anchor="start", ls=None):
    return (f'<text x="{x:.1f}" y="{y:.1f}" font-family=\'{FONT}\' '
            f'font-size="{size}" font-weight="{weight}" fill="{fill}" '
            f'text-anchor="{anchor}"'
            + (f' letter-spacing="{ls}"' if ls is not None else "")
            + f">{xesc(s)}</text>")


def _cube_parts(r=45):
    """等轴测立方：六边形轮廓 + 中心 Y 型接缝 + 顶面内切椭圆。

    接缝端点必须落在六边形顶点上，否则立方体读不出来 —— 故由顶点
    程序化推导，不手写坐标（手写坐标会随半径调整而漂移）。
    """
    v = ngon(50, 50, r, 6, -90)          # 顺序：T, UR, LR, B, LL, UL
    t, ur, _lr, b, _ll, ul = v
    return [
        ("polygon", v, "main"),
        ("path", f"M50 50 L{_f(ul[0])} {_f(ul[1])} "
                 f"M50 50 L{_f(ur[0])} {_f(ur[1])} "
                 f"M50 50 L{_f(b[0])} {_f(b[1])}", "accent"),
        # 顶面菱形 T-UR-C-UL 的内切椭圆：中心与半轴由顶点推导
        ("ellipse", (50, (t[1] + 50) / 2,
                     (ur[0] - ul[0]) / 4, (50 - t[1]) / 4), "secondary"),
    ]


# ---------------------------------------------------------------- 10 个语义角度
LOGOS = [
    {
        "key": "01-sphere-orbit", "zh": "全景环视", "angle": "空间覆盖",
        "concept": "球体经纬线框：球轮廓 + 经线 + 纬线。全景最本体的表达，语义零门槛。",
        "parts": [
            ("circle", (50, 50, 32), "main"),
            ("ellipse", (50, 50, 15, 32), "accent"),
            ("ellipse", (50, 50, 32, 15), "secondary"),
        ],
    },
    {
        "key": "02-mint-leaf", "zh": "薄荷全景", "angle": "品牌字面",
        "concept": "品牌名 Pano + mint 的字面双关：薄荷叶片轮廓内嵌全景线框球。形态独有、可注册性最强。",
        "parts": [
            # 叶片内嵌线框球（而非人字形叶脉）：既读成薄荷叶，又读成全景球
            ("path", "M50 5 C80 26 80 74 50 95 C20 74 20 26 50 5 Z", "main"),
            ("circle", (50, 50, 22), "accent"),
            ("ellipse", (50, 50, 10.5, 22), "secondary"),
        ],
    },
    {
        "key": "03-dual-lens", "zh": "双目成像", "angle": "成像原理",
        "concept": "两个交叠的鱼眼圆 + 中缝，直接指向 Insta360 双目合成 360 的成像方式。技术可信感强。",
        "parts": [
            ("circle", (36, 50, 29), "main"),
            ("circle", (64, 50, 29), "accent"),
            ("path", "M50 24.8 V75.2", "secondary"),
        ],
    },
    {
        "key": "04-equirect", "zh": "等距柱状", "angle": "投影数学",
        "concept": "全景展开后的扭曲网格：上下缘外弓、经线随球面外扩、赤道线居中。开放式网格，不闭合成容器。",
        "parts": [
            # 不用闭合轮廓：闭合后会被读成「木桶 / 鼓」，开放式网格才读成投影
            ("path", "M6 34 Q50 16 94 34", "main"),
            ("path", "M6 66 Q50 84 94 66", "main"),
            ("path", "M6 50 Q50 42 94 50", "secondary"),
            ("path", "M28 27.3 Q24.8 50 28 72.7", "secondary"),
            ("path", "M72 27.3 Q75.2 50 72 72.7", "secondary"),
        ],
    },
    {
        "key": "05-pano-window", "zh": "超宽画幅", "angle": "构图特征",
        "concept": "2.6:1 极宽取景框内嵌山峦地平线。用「画幅比例」而非相框表达全景，与方构图相册划清界限。",
        "parts": [
            ("rect", (4, 30, 92, 36, 5), "main"),
            ("polyline", [(12, 58), (28, 42), (39, 53), (51, 37), (63, 53), (88, 44)], "accent"),
            ("circle", (76, 41, 4.5), "secondary"),
        ],
    },
    {
        "key": "06-time-axis", "zh": "时光全景", "angle": "时间维度",
        "concept": "横轴串联 3 个全景球节点，中段放大形成主次。强调相册「按时间组织」，而非仅仅是个看图工具。",
        "parts": [
            ("line", (8, 50, 92, 50), "main"),
            ("circle", (24, 50, 12), "accent"),
            ("circle", (50, 50, 15), "accent"),
            ("circle", (76, 50, 12), "accent"),
            # 仅中段节点保留经线，避免 20px 下 6 个椭圆互相糊成墨团
            ("ellipse", (50, 50, 7, 15), "secondary"),
        ],
    },
    {
        "key": "07-spatial-cube", "zh": "全景立方", "angle": "空间维度",
        "concept": "等轴测立方线框，顶面承托全景球面投影。承载「空间 / VR 头追 / 陀螺仪」这层系统特色。",
        "parts": _cube_parts(),
    },
    {
        "key": "08-stitch", "zh": "全景拼接", "angle": "处理工艺",
        "concept": "三块竖板向外递减折叠，模拟全景由多帧缝合、向两侧包裹的空间关系。",
        "parts": [
            ("rect", (8, 24, 26, 52, 4), "secondary"),
            ("rect", (37, 18, 26, 64, 4), "accent"),
            ("rect", (66, 24, 26, 52, 4), "main"),
        ],
    },
    {
        "key": "09-ai-semantic", "zh": "智能语义", "angle": "智能维度",
        "concept": "球体 + AI 星芒。指向 Chinese-CLIP 中文语义检索，这是与竞品的硬差距所在。",
        "parts": [
            ("circle", (48, 54, 24), "main"),
            ("ellipse", (48, 54, 11.5, 24), "accent"),
            # 星芒须与球体明确分离并外移：贴着球体边长会在 16–24px 下粘成一团
            ("polygon", sparkle(82, 18, 13, 4.2), "secondary"),
        ],
    },
    {
        "key": "10-p-monogram", "zh": "字母标", "angle": "字形资产",
        "concept": "字母 P，碗部化为全景线框球。字形即品牌首字母，方形头像位与 favicon 场景最省事。",
        "parts": [
            ("path", "M26 6 V94", "main"),
            ("circle", (54, 32, 26), "main"),
            ("ellipse", (54, 32, 12.5, 26), "accent"),
            ("ellipse", (54, 32, 26, 12.5), "secondary"),
        ],
    },
]


# ---------------------------------------------------------------- 版式
def _wrap(s, n):
    out, cur = [], ""
    for ch in s:
        cur += ch
        if len(cur) >= n and ch in "，。、； ":
            out.append(cur)
            cur = ""
    if cur:
        out.append(cur)
    while len(out) < 2:
        out.append("")
    return out[:2]


def lockup_inner(mark, h):
    """横向组合：图形 + Panomint 字标"""
    m = int(h * 0.80)
    tx = m + int(h * 0.22)
    base = int(h * 0.56)
    return (
        f'<svg x="0" y="{(h - m) // 2}" width="{m}" height="{m}" '
        f'viewBox="0 0 100 100">{glyph_elements(mark)}</svg>'
        + txt(tx, base, "Panomint", size=int(h * 0.46), weight=300, ls=0.6)
        + txt(tx, base + int(h * 0.30), "全景相册系统",
              size=int(h * 0.17), weight=300, fill=TH["text2"], ls=int(h * 0.07))
    )


def sheet_main():
    """10 方案 ×（方标 / 横版组合 / 32px / 20px）"""
    pad, label_w = 44, 210
    cols = [(136, 108), (330, 84), (86, 32), (86, 20)]
    row_h = 132
    title_h, head_h, foot_h = 96, 46, 40
    width = pad + label_w + sum(c[0] for c in cols) + pad
    height = title_h + head_h + row_h * len(LOGOS) + foot_h

    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}">']
    o.append(txt(pad, 40, "Panomint 产品商标方案 · 10 个语义角度", 25, 600, TH["title"]))
    o.append(txt(pad, 62, "扁平 2D 矢量 · 彩色线条 · 透明底 · 无阴影 · 无噪点 · 100 网格", 13, 400, TH["text2"]))
    o.append(txt(pad, 82, "避让红线：风车花瓣（Google/Apple Photos）· 相框+山+太阳（Synology/Immich）· 棱镜（PhotoPrism）· 相机快门", 11, 400, TH["text3"]))
    o.append(f'<line x1="{pad}" y1="{title_h}" x2="{width - pad}" y2="{title_h}" stroke="{TH["border"]}"/>')

    x = pad + label_w
    for (cw, _), h in zip(cols, ["方形主标", "横向组合", "32px", "20px"]):
        o.append(txt(x + cw / 2, title_h + 28, h, 12, 400, TH["text2"], "middle"))
        x += cw
    o.append(f'<line x1="{pad}" y1="{title_h + head_h}" x2="{width - pad}" '
            f'y2="{title_h + head_h}" stroke="{TH["border"]}"/>')

    for i, mk in enumerate(LOGOS):
        top = title_h + head_h + row_h * i
        cy = top + row_h / 2
        o.append(f'<line x1="{pad}" y1="{top + row_h}" x2="{width - pad}" '
                 f'y2="{top + row_h}" stroke="{TH["hair"]}"/>')
        o.append(txt(pad, cy - 22, mk["key"].split("-")[0], 11, 400, TH["text3"], ls=1))
        o.append(txt(pad, cy - 2, mk["zh"], 17, 600, TH["ink"]))
        o.append(txt(pad, cy + 16, "角度 · " + mk["angle"], 11, 400, TH["text2"]))
        w1, w2 = _wrap(mk["concept"], 17)
        o.append(txt(pad, cy + 32, w1, 10, 400, TH["text3"]))
        o.append(txt(pad, cy + 44, w2, 10, 400, TH["text3"]))

        cx = pad + label_w
        for cw, sz in cols:
            if sz == 84:  # 横向组合列
                lh = 84
                o.append(f'<svg x="{cx + 12}" y="{cy - lh / 2}" width="310" '
                       f'height="{lh}" viewBox="0 0 310 {lh}">{lockup_inner(mk, lh)}</svg>')
            else:
                o.append(embed(mk, cx + (cw - sz) / 2, cy - sz / 2, sz))
            cx += cw

    o.append(txt(pad, height - 14, "Panomint · 商标方案样图 · scripts/gen_logo_marks.py",
                 11, 400, TH["text3"]))
    o.append("</svg>")
    return "".join(o), width, height


def sheet_sizes(darken=0.0):
    """10 方案 ×（16 / 24 / 32 / 48px）真实尺寸对照"""
    pad, label_w = 44, 200
    sizes = [16, 24, 32, 48]
    cell = 92
    row_h = 84
    title_h, head_h, foot_h = 92, 44, 40
    width = pad + label_w + cell * len(sizes) + pad
    height = title_h + head_h + row_h * len(LOGOS) + foot_h

    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}">']
    o.append(txt(pad, 38, "真实尺寸对照表", 24, 600, TH["title"]))
    o.append(txt(pad, 60, "商标的生死线在 16–24px：放大好看不算数，缩小能认出来才算数。",
                 12, 400, TH["text2"]))
    o.append(f'<line x1="{pad}" y1="{title_h}" x2="{width - pad}" y2="{title_h}" '
            f'stroke="{TH["border"]}"/>')
    for j, s in enumerate(sizes):
        cx = pad + label_w + cell * j + cell / 2
        o.append(txt(cx, title_h + 28, "%dpx" % s, 12, 400, TH["text2"], "middle"))
    o.append(f'<line x1="{pad}" y1="{title_h + head_h}" x2="{width - pad}" '
            f'y2="{title_h + head_h}" stroke="{TH["border"]}"/>')

    for i, mk in enumerate(LOGOS):
        top = title_h + head_h + row_h * i
        cy = top + row_h / 2
        o.append(f'<line x1="{pad}" y1="{top + row_h}" x2="{width - pad}" '
                 f'y2="{top + row_h}" stroke="{TH["hair"]}"/>')
        o.append(txt(pad, cy - 4, "%s %s" % (mk["key"].split("-")[0], mk["zh"]),
                 14, 600, TH["ink"]))
        o.append(txt(pad, cy + 14, "角度 · " + mk["angle"], 10.5, 400, TH["text3"]))
        for j, s in enumerate(sizes):
            cx = pad + label_w + cell * j + (cell - s) / 2
            o.append(embed(mk, cx, cy - s / 2, s, darken=darken))
    o.append(txt(pad, height - 14, "Panomint · 商标方案样图 · scripts/gen_logo_marks.py",
                 11, 400, TH["text3"]))
    o.append("</svg>")
    return "".join(o), width, height


def sheet_dark():
    """暗色页预览：底色 #14181d，各色按 HSL 提亮 58%"""
    pad, label_w = 44, 210
    cols = [(136, 96), (330, 72)]
    row_h = 108
    title_h, head_h, foot_h = 92, 44, 40
    width = pad + label_w + sum(c[0] for c in cols) + pad
    height = title_h + head_h + row_h * len(LOGOS) + foot_h

    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}">',
         f'<rect width="{width}" height="{height}" fill="#14181d"/>']
    o.append(txt(pad, 38, "暗色页适配预览 · player / share", 24, 600, THD["title"]))
    o.append(txt(pad, 60, "底色 #14181d · 各色按 HSL 提亮 58% 模拟暗色 token 换色（同色直出会不可见）",
                 12, 400, THD["text2"]))
    o.append(f'<line x1="{pad}" y1="{title_h}" x2="{width - pad}" y2="{title_h}" '
            f'stroke="{THD["border"]}"/>')
    x = pad + label_w
    for (cw, _), h in zip(cols, ["方形主标", "横向组合"]):
        o.append(txt(x + cw / 2, title_h + 28, h, 12, 400, THD["text2"], "middle"))
        x += cw
    o.append(f'<line x1="{pad}" y1="{title_h + head_h}" x2="{width - pad}" '
            f'y2="{title_h + head_h}" stroke="{THD["border"]}"/>')

    for i, mk in enumerate(LOGOS):
        top = title_h + head_h + row_h * i
        cy = top + row_h / 2
        o.append(f'<line x1="{pad}" y1="{top + row_h}" x2="{width - pad}" '
                 f'y2="{top + row_h}" stroke="{THD["hair"]}"/>')
        o.append(txt(pad, cy + 4, "%s %s" % (mk["key"].split("-")[0], mk["zh"]),
                 13, 600, THD["ink"]))
        cx = pad + label_w
        for cw, sz in cols:
            if cw == 330:
                o.append(f'<svg x="{cx + 8}" y="{cy - 72 / 2}" width="300" height="72" '
                       f'viewBox="0 0 300 72">{lockup_inner(mk, 72)}</svg>')
            else:
                o.append(embed(mk, cx + (cw - sz) / 2, cy - sz / 2, sz, darken=0.58))
            cx += cw

    o.append(txt(pad, height - 14, "Panomint · 商标方案样图 · 暗色底为补充预览，主交付为透明底 PNG",
                 11, 400, THD["text3"]))
    o.append("</svg>")
    return "".join(o), width, height


def plate_svg(mark: dict):
    """单方案大图：方标 + 横版组合（并排）+ 尺寸条 + 语义说明"""
    pad, sq, lh, lw = 44, 216, 96, 340
    title_y = 58
    mark_y = title_y + 36
    lock_x = pad + sq + 56
    lock_y = mark_y + (sq - lh) / 2
    bar_y = mark_y + sq + 44
    note_y = bar_y + 84

    width = pad + sq + 56 + lw + pad
    height = int(note_y + 48)

    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}">']
    o.append(txt(pad, title_y, "%s  ·  %s" % (mark["key"], mark["zh"]), 21, 600, TH["title"]))
    o.append(txt(pad, mark_y - 12, "方形主标", 11, 400, TH["text3"]))
    o.append(embed(mark, pad, mark_y, sq))

    o.append(txt(lock_x, lock_y - 12, "横向组合", 11, 400, TH["text3"]))
    o.append(f'<svg x="{lock_x}" y="{_f(lock_y)}" width="{lw}" height="{lh}" '
             f'viewBox="0 0 {lw} {lh}">{lockup_inner(mark, lh)}</svg>')

    bx = pad
    o.append(txt(pad, bar_y - 6, "小尺寸可读性（16 / 24 / 32 / 48px）", 11, 400, TH["text3"]))
    for s in (16, 24, 32, 48):
        o.append(embed(mark, bx, bar_y + 6, s))
        o.append(txt(bx, bar_y + 6 + s + 16, str(s), 10, 400, TH["text3"]))
        bx += max(s + 40, 62)

    o.append(txt(pad, note_y, "语义角度：" + mark["angle"], 12.5, 600, TH["ink"]))
    for k, ln in enumerate(_wrap(mark["concept"], 64)):
        o.append(txt(pad, note_y + 20 + k * 17, ln, 12, 400, TH["text2"]))
    o.append("</svg>")
    return "".join(o), width, height


# ---------------------------------------------------------------- 光栅化
CHROME_CANDIDATES = [
    r"C:/Program Files/Google/Chrome/Application/chrome.exe",
    r"C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
    r"C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
    r"C:/Program Files/Microsoft/Edge/Application/msedge.exe",
]


def find_chrome() -> str:
    env = os.environ.get("CHROME_BIN")
    if env and os.path.exists(env):
        return env
    for p in CHROME_CANDIDATES:
        if os.path.exists(p):
            return p
    return ""


def png_info(path: str):
    with open(path, "rb") as f:
        head = f.read(33)
    if head[:8] != b"\x89PNG\r\n\x1a\n":
        return None
    w, h = struct.unpack(">II", head[16:24])
    return w, h, head[24], head[25]


def rasterize(svg_text, out_png, width, height, scale=2, chrome=""):
    tmp_dir = os.path.join(os.path.dirname(out_png), ".tmp_render")
    os.makedirs(tmp_dir, exist_ok=True)
    html_path = os.path.join(tmp_dir, os.path.basename(out_png) + ".html")
    with open(html_path, "w", encoding="utf-8") as f:
        f.write('<!doctype html><html><head><meta charset="utf-8">'
                '<style>html,body{margin:0;padding:0;background:transparent;}'
                'svg{display:block;}</style></head><body>' + svg_text + '</body></html>')
    cmd = [chrome, "--headless=new", "--disable-gpu", "--no-sandbox",
           "--disable-dev-shm-usage", "--hide-scrollbars",
           "--force-device-scale-factor=%d" % scale,
           "--window-size=%d,%d" % (width, height),
           "--default-background-color=00000000",
           "--screenshot=" + out_png.replace("\\", "/"),
           "file:///" + html_path.replace("\\", "/")]
    try:
        subprocess.run(cmd, check=False, stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, timeout=180)
    except Exception as exc:
        print("  render error:", exc)
        return False
    return os.path.exists(out_png)


# ---------------------------------------------------------------- 主流程
def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--svg-only", action="store_true")
    ap.add_argument("--check-only", action="store_true")
    ap.add_argument("--only-matrix", action="store_true")
    ap.add_argument("--only", default="",
                    help="只渲染匹配的名字（逗号分隔子串，如 sheet,09）——改一处不必重跑全部")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    root = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    out_dir = args.out or os.path.join(root, "文档", "商标设计")
    os.makedirs(out_dir, exist_ok=True)

    chrome = "" if (args.svg_only or args.check_only) else find_chrome()
    if not (args.svg_only or args.check_only) and not chrome:
        print("FATAL: 未找到 Chrome/Edge，请设置 CHROME_BIN 或改用 --svg-only")
        return 2

    jobs = [("sheet_logos",) + sheet_main(),
            ("sheet_sizes",) + sheet_sizes()]
    if not args.only_matrix:
        jobs.append(("sheet_dark",) + sheet_dark())
        for mk in LOGOS:
            jobs.append(("logo_" + mk["key"],) + plate_svg(mk))

    # 有意铺底色的图：不参与「必须透明」断言
    opaque_ok = {os.path.join(out_dir, "sheet_dark.png")}

    if args.only:
        keys = [s.strip() for s in args.only.split(",") if s.strip()]
        jobs = [j for j in jobs if any(k in j[0] for k in keys)]
        if not jobs:
            print("FATAL: --only 未匹配到任何任务")
            return 2
        print("  已筛选：%s" % [j[0] for j in jobs])

    made_svg, made_png = [], []
    for name, svg_text, w, h in jobs:
        p_svg = os.path.join(out_dir, name + ".svg")
        if not args.check_only:
            with open(p_svg, "w", encoding="utf-8") as f:
                f.write(svg_text)
            made_svg.append(p_svg)
        p_png = os.path.join(out_dir, name + ".png")
        if args.check_only:
            if os.path.exists(p_png):
                made_png.append(p_png)
            continue
        if args.svg_only:
            continue
        print("  rendering", name, "...", flush=True)
        if rasterize(svg_text, p_png, w, h, 2, chrome):
            made_png.append(p_png)
        else:
            print("  FAILED", name)

    print("\n=== SVG (%d) ===" % len(made_svg))
    for p in made_svg:
        print("  %-44s %8d B" % (os.path.basename(p), os.path.getsize(p)))

    if made_png:
        print("\n=== PNG (%d) ===" % len(made_png))
        bad = []
        for p in made_png:
            info = png_info(p)
            if not info:
                bad.append(p)
                print("  BROKEN", os.path.basename(p))
                continue
            w, h, depth, ctype = info
            need_alpha = p not in opaque_ok
            print("  %-44s %dx%d %-9s %8d B"
                  % (os.path.basename(p), w, h,
                     "RGBA" if ctype == 6 else "RGB(铺底)",
                     os.path.getsize(p)))
            if need_alpha and ctype != 6:
                bad.append(p)
        print("\nRESULT:", "ALL-OK" if not bad else "FAILED=%s" % bad)
        return 0 if not bad else 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
