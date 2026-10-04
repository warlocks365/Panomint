#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Panomint 图标风格样图生成器（图标设计任务）

设计约束（用户指令 + 项目 P0 规则）：
  - 扁平化、2D 矢量、SVG 风格、无噪点
  - 纯白 / 透明背景（PNG 为透明底 RGBA）
  - 彩色线条（以描边为主，fill 仅用于 ≤14% 的淡底点缀）
  - 简约、干净轮廓、无多余阴影

产出：
  文档/图标设计/sheet_all.png        10 风格 x 12 图标 @42px（总览）
  文档/图标设计/sheet_true24.png     12 图标 x 10 风格 @真实 24px（选型决策矩阵）
  文档/图标设计/sheet_dark.png       10 风格 x 12 图标 @36px 暗色页适配预览
  文档/图标设计/style_NN_*.png       每套独立样图（含真实 24px 参照行）

用法：
  python scripts/gen_icon_sheets.py
  python scripts/gen_icon_sheets.py --svg-only
  python scripts/gen_icon_sheets.py --skip-strips          # 只出 3 张矩阵图，快
  python scripts/gen_icon_sheets.py --export-style 07      # 导出该风格 12 个独立 24x24 SVG

依赖：仅标准库；光栅化调用本机 Chrome headless（--default-background-color=00000000）。
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

# ---------------------------------------------------------------- 调色板
# 沿用「晨雾 Morandi」方案 H（src/frontend/src/styles/tokens.css）：
# 全站饱和度封顶、无纯黑纯白，靠明度与 hairline 分层，不靠阴影。
P = {
    "ink": "#41403c",      # 炭灰（主线条）
    "mist": "#4a5a6a",     # 深雾蓝（品牌主色）
    "sky": "#7d95a8",      # 雾蓝（次级）
    "violet": "#9a8aa0",   # 灰紫褐
    "oat": "#c4a57a",      # 燕麦（全景照片）
    "clay": "#b0766a",     # 陶土红（全景视频）
    "moss": "#6e8a72",     # 灰绿
}

THEME_LIGHT = {
    "border": "#ddd8d0", "hair": "#e9e4de",
    "ink": "#41403c", "text2": "#7d7a73", "text3": "#a5a29a", "title": "#41403c",
}
THEME_DARK = {
    "border": "#2c333b", "hair": "#232931",
    "ink": "#e6ebf0", "text2": "#9aa7b4", "text3": "#6f7c89", "title": "#f2f5f8",
}


# ---------------------------------------------------------------- 工具
def _fmt(v: float) -> str:
    return f"{v:.2f}".rstrip("0").rstrip(".")


def _pts(pairs) -> str:
    return " ".join(f"{_fmt(x)},{_fmt(y)}" for x, y in pairs)


def hex_to_hsl(h: str):
    h = h.lstrip("#")
    r, g, b = (int(h[i:i + 2], 16) / 255.0 for i in (0, 2, 4))
    return colorsys.rgb_to_hls(r, g, b)


def lighten_hex(hex_color: str, amount: float) -> str:
    """按 HSL 提亮（保住色相与饱和度），用于暗色底预览的 token 换色"""
    h, l, s = hex_to_hsl(hex_color)
    l = l + (1.0 - l) * amount
    r, g, b = colorsys.hls_to_rgb(h, min(l, 1.0), s)
    return "#%02x%02x%02x" % (round(r * 255), round(g * 255), round(b * 255))


def octagon_pts(cx, cy, rx, ry):
    """正八边形逼近（几何硬角风格：圆 → 八边形）"""
    return [
        (cx + rx * math.cos(math.radians(22.5 + 45 * k)),
         cy + ry * math.sin(math.radians(22.5 + 45 * k)))
        for k in range(8)
    ]


def star_pts(cx, cy, ro, ri, n=5):
    out = []
    for k in range(n * 2):
        r = ro if k % 2 == 0 else ri
        a = math.radians(-90 + 180 * k / n)
        out.append((cx + r * math.cos(a), cy + r * math.sin(a)))
    return out


def sparkle_pts(cx, cy, ro, ri):
    """四角闪光（AI 语义特征）"""
    out = []
    for k in range(8):
        r = ro if k % 2 == 0 else ri
        a = math.radians(-90 + 45 * k)
        out.append((cx + r * math.cos(a), cy + r * math.sin(a)))
    return out


# ---------------------------------------------------------------- 图元
def primitive(kind: str, params, geom: bool):
    """返回 (tag, attrs) —— 坐标一律在 24x24 用户空间"""
    if kind == "path":
        return "path", {"d": params}
    if kind == "circle":
        cx, cy, r = params
        if geom and r >= 2.4:
            return "polygon", {"points": _pts(octagon_pts(cx, cy, r, r))}
        return "circle", {"cx": _fmt(cx), "cy": _fmt(cy), "r": _fmt(r)}
    if kind == "ellipse":
        cx, cy, rx, ry = params
        if geom:
            return "polygon", {"points": _pts(octagon_pts(cx, cy, rx, ry))}
        return "ellipse", {"cx": _fmt(cx), "cy": _fmt(cy),
                           "rx": _fmt(rx), "ry": _fmt(ry)}
    if kind == "ellipse-rot":
        cx, cy, rx, ry, deg = params
        t = f"rotate({_fmt(deg)} {_fmt(cx)} {_fmt(cy)})"
        if geom:
            return "polygon", {"points": _pts(octagon_pts(cx, cy, rx, ry)),
                               "transform": t}
        return "ellipse", {"cx": _fmt(cx), "cy": _fmt(cy), "rx": _fmt(rx),
                           "ry": _fmt(ry), "transform": t}
    if kind == "rect":
        x, y, w, h, rx = params
        return "rect", {"x": _fmt(x), "y": _fmt(y), "width": _fmt(w),
                        "height": _fmt(h), "rx": _fmt(0 if geom else rx)}
    if kind == "line":
        x1, y1, x2, y2 = params
        return "line", {"x1": _fmt(x1), "y1": _fmt(y1),
                        "x2": _fmt(x2), "y2": _fmt(y2)}
    if kind == "polyline":
        return "polyline", {"points": _pts(params)}
    if kind == "polygon":
        return "polygon", {"points": _pts(params)}
    if kind == "dotcircle":
        cx, cy, r = params
        if geom:
            return "polygon", {"points": _pts(octagon_pts(cx, cy, r, r))}
        return "circle", {"cx": _fmt(cx), "cy": _fmt(cy), "r": _fmt(r)}
    raise ValueError("unknown primitive: " + kind)


def is_closed(kind: str, params) -> bool:
    if kind in ("circle", "ellipse", "ellipse-rot", "rect", "polygon", "dotcircle"):
        return True
    if kind == "path":
        return params.strip().upper().endswith("Z")
    return False


# ---------------------------------------------------------------- 图标语义集
# 12 个突出 Panomint 特色的语义，其中 4 个为全景专属：
#   全景 / 360视频 / 空间（VR 头追·陀螺仪）/ AI 搜索（Chinese-CLIP 语义检索）
# role: main 轮廓主体 / accent 语义重点 / secondary 辅助细节
ICONS = [
    {
        "key": "panorama", "zh": "全景",
        "parts": [
            ("circle", (12, 12, 9.1), "main"),
            ("ellipse", (12, 12, 4.3, 9.1), "accent"),
            ("ellipse", (12, 12, 9.1, 4.3), "secondary"),
        ],
    },
    {
        "key": "pano-video", "zh": "360视频",
        # 24px 可读性修正：去掉斜轨道椭圆（小尺寸下糊成「眼睛」），
        # 改为「球体 + 播放键 + 赤道两侧自转短线」表达 360° 环绕播放。
        "parts": [
            ("circle", (12, 12, 8.3), "main"),
            ("polygon", [(10.3, 8.6), (15.6, 12), (10.3, 15.4)], "accent"),
            ("path", "M1.5 12 H3.6 M20.4 12 H22.5", "secondary"),
        ],
    },
    {
        "key": "album", "zh": "相册",
        "parts": [
            ("rect", (2.5, 5.3, 19.0, 13.4, 2.2), "main"),
            ("polyline", [(5.2, 15.7), (9.1, 10.8), (12.1, 13.8),
                          (15.1, 9.8), (19.0, 14.9)], "accent"),
            ("circle", (7.4, 8.6, 1.4), "secondary"),
        ],
    },
    {
        "key": "timeline", "zh": "时间线",
        # 24px 可读性修正：节点由描边圆环改为实心小圆点（环在 24px 下糊成一串珠子）
        "parts": [
            ("line", (5.4, 4.0, 5.4, 20.0), "main"),
            ("dotcircle", (5.4, 7.5, 1.4), "accent"),
            ("dotcircle", (5.4, 12.0, 1.4), "accent"),
            ("dotcircle", (5.4, 16.5, 1.4), "accent"),
            ("line", (9.4, 7.5, 19.6, 7.5), "secondary"),
            ("line", (9.4, 12.0, 17.6, 12.0), "secondary"),
            ("line", (9.4, 16.5, 18.8, 16.5), "secondary"),
        ],
    },
    {
        "key": "people", "zh": "人物",
        "parts": [
            ("circle", (8.4, 7.9, 3.3), "main"),
            ("path", "M2.4 19.7 C2.4 15.6 5.2 12.4 8.6 12.4 C12.0 12.4 14.8 15.6 14.8 19.7", "main"),
            ("circle", (16.6, 10.2, 2.2), "accent"),
            ("path", "M13.4 19.7 C13.4 17.3 14.7 15.4 16.5 15.4 C18.3 15.4 19.6 17.3 19.6 19.7", "accent"),
        ],
    },
    {
        "key": "place", "zh": "地点",
        "parts": [
            ("path", "M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 0 1 16 0Z", "main"),
            ("circle", (12, 10, 3.0), "accent"),
        ],
    },
    {
        "key": "space", "zh": "空间",
        "parts": [
            ("polygon", [(12, 3.0), (20.4, 7.75), (20.4, 16.75),
                         (12, 21.5), (3.6, 16.75), (3.6, 7.75)], "main"),
            ("path", "M12 12.25 L3.6 7.75 M12 12.25 L20.4 7.75 M12 12.25 L12 21.5", "accent"),
        ],
    },
    {
        "key": "slideshow", "zh": "幻灯片",
        "parts": [
            ("rect", (2.2, 4.8, 19.6, 14.4, 2.2), "main"),
            ("polygon", [(9.4, 9.6), (14.6, 12.0), (9.4, 14.4)], "accent"),
            ("path", "M16.4 9.6 L18.9 12 L16.4 14.4", "secondary"),
        ],
    },
    {
        "key": "star", "zh": "收藏",
        # 24px 可读性修正：星形略缩、闪光点外移，避免小尺寸下两点粘连成噪点
        "parts": [
            ("polygon", star_pts(11.5, 12.9, 8.2, 3.4), "main"),
            ("polygon", sparkle_pts(19.9, 4.3, 2.3, 0.75), "accent"),
        ],
    },
    {
        "key": "camera", "zh": "相机",
        "parts": [
            ("rect", (2.2, 6.4, 19.6, 13.2, 2.8), "main"),
            ("path", "M8.6 6.4 L9.9 4.0 H14.1 L15.4 6.4", "main"),
            ("circle", (12, 13.0, 3.8), "accent"),
            ("dotcircle", (17.9, 9.4, 1.05), "secondary"),
        ],
    },
    {
        "key": "gps", "zh": "GPS定位",
        "parts": [
            ("circle", (12, 12, 6.6), "main"),
            ("path", "M12 2.6 V5.4 M12 18.6 V21.4 M2.6 12 H5.4 M18.6 12 H21.4", "secondary"),
            ("dotcircle", (12, 12, 2.0), "accent"),
        ],
    },
    {
        "key": "search-ai", "zh": "AI搜索",
        # 24px 可读性修正：闪光缩小并外移，避免与手柄连成形似「Q」的粘连
        "parts": [
            ("circle", (10.6, 10.6, 6.6), "main"),
            ("path", "M15.4 15.4 L20.2 20.2", "main"),
            ("polygon", sparkle_pts(19.0, 4.4, 2.5, 0.8), "accent"),
        ],
    },
]


# ---------------------------------------------------------------- 风格定义
def _c(main, accent, secondary):
    return {"main": main, "accent": accent, "secondary": secondary}


STYLES = [
    {"key": "01-hairline-mono", "zh": "细线 · 雾蓝单色", "desc": "1.15 描边 · 全单色",
     "sw": 1.15, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["mist"], P["mist"])},

    {"key": "02-standard", "zh": "标准 · 炭灰线框", "desc": "1.70 描边 · 主次蓝灰",
     "sw": 1.70, "cap": "round", "join": "round",
     "colors": _c(P["ink"], P["mist"], P["sky"])},

    {"key": "03-duotone", "zh": "双色 · 雾蓝×燕麦", "desc": "1.80 描边 · 两色分工",
     "sw": 1.80, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["oat"], P["oat"]),
     "op": {"secondary": 0.5}},

    {"key": "04-multicolor", "zh": "多色 · 三色分区", "desc": "1.80 描边 · 分区着色",
     "sw": 1.80, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["clay"], P["moss"])},

    {"key": "05-bold-round", "zh": "粗圆头 · 亲和", "desc": "2.60 描边 · 圆头圆角",
     "sw": 2.60, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["sky"], P["oat"])},

    {"key": "06-geometric", "zh": "几何 · 硬角网格", "desc": "1.50 描边 · 圆转八边形",
     "sw": 1.50, "cap": "butt", "join": "miter",
     "colors": _c(P["ink"], P["mist"], P["sky"]),
     "geom": True},

    {"key": "07-outline-tint", "zh": "描边 · 淡彩填充", "desc": "1.70 描边 · 14% 淡底",
     "sw": 1.70, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["oat"], P["sky"]),
     "tint": 0.14},

    {"key": "08-double-line", "zh": "内外双线 · 嵌套", "desc": "外 20% 光晕 + 内实线",
     "sw": 1.40, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["clay"], P["oat"]),
     "double": True},

    {"key": "09-dashed", "zh": "断续线 · 节拍", "desc": "1.80 描边 · 虚线分段",
     "sw": 1.80, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["oat"], P["clay"]),
     "dash": "2.2 1.8"},

    {"key": "10-badge", "zh": "徽章 · 圆角底座", "desc": "1.60 描边 · 底座 66%",
     "sw": 1.60, "cap": "round", "join": "round",
     "colors": _c(P["mist"], P["clay"], P["sky"]),
     "badge": True},
]

BADGE_SCALE = 0.66


# ---------------------------------------------------------------- 渲染
def _attrs(attrs: dict) -> str:
    return " ".join(f'{k}="{v}"' for k, v in attrs.items())


def glyph_body(icon: dict, style: dict, darken: float = 0.0) -> str:
    """darken>0 时按 HSL 提亮各色（暗色底 token 换色预览）"""
    def col_of(role):
        c = style["colors"][role]
        return lighten_hex(c, darken) if darken else c

    op = style.get("op", {})
    sw = style["sw"]
    cap, join = style["cap"], style["join"]
    dash = style.get("dash")
    geom = style.get("geom", False)
    tint = style.get("tint", 0.0)
    double = style.get("double", False)

    out = []
    for kind, params, role in icon["parts"]:
        col = col_of(role)
        tag, attrs = primitive(kind, params, geom)
        a = _attrs(attrs)

        if kind == "dotcircle":
            out.append(f'<circle {a} fill="{col}"/>')
            continue

        if double:
            # 外层低透明度粗线（光晕）+ 内层实线；全程扁平，无阴影滤镜
            out.append(
                f'<{tag} {a} fill="none" stroke="{col}" stroke-opacity="0.20" '
                f'stroke-width="{sw * 2.6:.2f}" stroke-linecap="round" '
                f'stroke-linejoin="round"/>')
            out.append(
                f'<{tag} {a} fill="none" stroke="{col}" stroke-width="{sw:.2f}" '
                f'stroke-linecap="round" stroke-linejoin="round"/>')
            continue

        if tint and is_closed(kind, params):
            fill_attr = f'fill="{col}" fill-opacity="{tint}"'
        else:
            fill_attr = 'fill="none"'

        dsh = f' stroke-dasharray="{dash}"' if dash else ""
        o = op.get(role)
        opa = f' stroke-opacity="{o}"' if o else ""
        out.append(
            f'<{tag} {a} {fill_attr} stroke="{col}" stroke-width="{sw:.2f}" '
            f'stroke-linecap="{cap}" stroke-linejoin="{join}"{dsh}{opa}/>')
    return "".join(out)


def glyph_elements(icon: dict, style: dict, darken: float = 0.0) -> str:
    """返回 <svg> 内部元素（不含外层 svg 标签），viewBox 固定 0 0 24 24"""
    body = glyph_body(icon, style, darken)
    if style.get("badge"):
        sky = lighten_hex(P["sky"], darken) if darken else P["sky"]
        frame = (f'<rect x="1.5" y="1.5" width="21" height="21" rx="6.5" '
                 f'fill="none" stroke="{sky}" stroke-opacity="0.42" '
                 f'stroke-width="1.2" stroke-linecap="round"/>')
        body = (f'<g transform="translate(12 12) scale({BADGE_SCALE}) '
                f'translate(-12 -12)">{body}</g>')
        return frame + body
    return body


def glyph_svg(icon: dict, style: dict, size: float, darken: float = 0.0) -> str:
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" '
            f'width="{_fmt(size)}" height="{_fmt(size)}" fill="none">'
            f'{glyph_elements(icon, style, darken)}</svg>')


# ---------------------------------------------------------------- 版式
FONT = '"Microsoft YaHei","PingFang SC","Noto Sans SC","Helvetica Neue",sans-serif'


def grid_svg(styles, *, glyph, row_h, label_w, cell, head_h, title, subtitle,
             footer, pad=40, title_h=84, foot_h=36, theme=None, bg=None,
             compact=False, darken=0.0):
    """行 = 风格，列 = 图标"""
    th = theme or THEME_LIGHT
    width = pad + label_w + cell * len(ICONS) + pad
    height = title_h + head_h + row_h * len(styles) + foot_h
    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}" '
         f"font-family='{FONT}'>"]
    if bg:
        o.append(f'<rect x="0" y="0" width="{width}" height="{height}" fill="{bg}"/>')
    o.append(f'<text x="{pad}" y="{title_h - 26}" font-size="26" font-weight="600" '
             f'fill="{th["title"]}">{xesc(title)}</text>')
    o.append(f'<text x="{pad}" y="{title_h - 8}" font-size="12" '
             f'fill="{th["text2"]}">{xesc(subtitle)}</text>')
    o.append(f'<line x1="{pad}" y1="{title_h}" x2="{width - pad}" y2="{title_h}" '
             f'stroke="{th["border"]}" stroke-width="1"/>')

    for i, ic in enumerate(ICONS):
        cx = pad + label_w + cell * i + cell / 2
        o.append(f'<text x="{cx:.1f}" y="{title_h + head_h / 2 + 4:.1f}" '
                 f'font-size="12" fill="{th["text2"]}" text-anchor="middle">'
                 f'{xesc(ic["zh"])}</text>')
    o.append(f'<line x1="{pad}" y1="{title_h + head_h}" x2="{width - pad}" '
             f'y2="{title_h + head_h}" stroke="{th["border"]}" stroke-width="1"/>')

    for si, st in enumerate(styles):
        top = title_h + head_h + row_h * si
        cy = top + row_h / 2
        o.append(f'<line x1="{pad}" y1="{top + row_h}" x2="{width - pad}" '
                 f'y2="{top + row_h}" stroke="{th["hair"]}" stroke-width="1"/>')
        num = st["key"].split("-")[0]
        if compact:
            o.append(f'<text x="{pad}" y="{cy + 4:.1f}" font-size="13" '
                     f'font-weight="600" fill="{th["ink"]}">{num} {xesc(st["zh"])}</text>')
        else:
            o.append(f'<text x="{pad}" y="{cy - 14:.1f}" font-size="11" '
                     f'fill="{th["text3"]}" letter-spacing="1">{num}</text>')
            o.append(f'<text x="{pad}" y="{cy + 5:.1f}" font-size="16" '
                     f'font-weight="600" fill="{th["ink"]}">{xesc(st["zh"])}</text>')
            o.append(f'<text x="{pad}" y="{cy + 21:.1f}" font-size="11" '
                     f'fill="{th["text2"]}">{xesc(st["desc"])}</text>')
        for i, ic in enumerate(ICONS):
            gx = pad + label_w + cell * i + (cell - glyph) / 2
            gy = top + (row_h - glyph) / 2
            o.append(f'<svg x="{gx:.1f}" y="{gy:.1f}" width="{_fmt(glyph)}" '
                     f'height="{_fmt(glyph)}" viewBox="0 0 24 24" fill="none">'
                     f'{glyph_elements(ic, st, darken)}</svg>')

    o.append(f'<text x="{pad}" y="{height - 12}" font-size="11" '
             f'fill="{th["text3"]}">{xesc(footer)}</text>')
    o.append("</svg>")
    return "".join(o), width, height


def matrix_svg(styles, *, glyph=24, row_h=46, label_w=120, cell=118, pad=40,
               title_h=84, head_h=52, foot_h=36, title="", subtitle="", footer=""):
    """行 = 图标，列 = 风格；glhpy 固定 24px —— 真实尺寸选型决策矩阵"""
    th = THEME_LIGHT
    width = pad + label_w + cell * len(styles) + pad
    height = title_h + head_h + row_h * len(ICONS) + foot_h
    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}" '
         f"font-family='{FONT}'>"]
    o.append(f'<text x="{pad}" y="{title_h - 26}" font-size="26" font-weight="600" '
             f'fill="{th["title"]}">{xesc(title)}</text>')
    o.append(f'<text x="{pad}" y="{title_h - 8}" font-size="12" fill="{th["text2"]}">'
             f'{xesc(subtitle)}</text>')
    o.append(f'<line x1="{pad}" y1="{title_h}" x2="{width - pad}" y2="{title_h}" '
             f'stroke="{th["border"]}" stroke-width="1"/>')

    for si, st in enumerate(styles):
        cx = pad + label_w + cell * si + cell / 2
        parts = st["zh"].split(" · ")
        o.append(f'<text x="{cx:.1f}" y="{title_h + 20}" font-size="11" '
                 f'fill="{th["text3"]}" text-anchor="middle" letter-spacing="1">'
                 f'{st["key"].split("-")[0]}</text>')
        o.append(f'<text x="{cx:.1f}" y="{title_h + 38}" font-size="13" '
                 f'font-weight="600" fill="{th["ink"]}" text-anchor="middle">'
                 f'{xesc(parts[-1])}</text>')
    o.append(f'<line x1="{pad}" y1="{title_h + head_h}" x2="{width - pad}" '
             f'y2="{title_h + head_h}" stroke="{th["border"]}" stroke-width="1"/>')

    for ii, ic in enumerate(ICONS):
        top = title_h + head_h + row_h * ii
        cy = top + row_h / 2
        o.append(f'<line x1="{pad}" y1="{top + row_h}" x2="{width - pad}" '
                 f'y2="{top + row_h}" stroke="{th["hair"]}" stroke-width="1"/>')
        o.append(f'<text x="{pad}" y="{cy + 4:.1f}" font-size="13" '
                 f'fill="{th["ink"]}">{xesc(ic["zh"])}</text>')
        for si, st in enumerate(styles):
            gx = pad + label_w + cell * si + (cell - glyph) / 2
            gy = cy - glyph / 2
            o.append(f'<svg x="{gx:.1f}" y="{gy:.1f}" width="{_fmt(glyph)}" '
                     f'height="{_fmt(glyph)}" viewBox="0 0 24 24" fill="none">'
                     f'{glyph_elements(ic, st)}</svg>')
    o.append(f'<text x="{pad}" y="{height - 12}" font-size="11" fill="{th["text3"]}">'
             f'{xesc(footer)}</text>')
    o.append("</svg>")
    return "".join(o), width, height


def strip_svg(style: dict):
    """单套风格独立样图：主行 42px + 真实 24px 参照行"""
    th = THEME_LIGHT
    pad, cell, row_h = 40, 78, 98
    glyph = 42
    width = pad + cell * len(ICONS) + pad
    head_top, head_h = 56, 34
    ref_label_y = head_top + head_h + row_h + 18 + 16
    height = ref_label_y + 26 + 12 + 24
    o = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" '
         f'height="{height}" viewBox="0 0 {width} {height}" font-family=\'{FONT}\'>']
    o.append(f'<text x="{pad}" y="34" font-size="20" font-weight="600" '
             f'fill="{th["title"]}">{xesc(style["key"] + " · " + style["zh"])}</text>')
    o.append(f'<text x="{pad}" y="52" font-size="12" fill="{th["text2"]}">'
             f'{xesc(style["desc"] + " · 24 网格 · 彩色描边 · 透明底 · 无阴影")}</text>')
    o.append(f'<line x1="{pad}" y1="{head_top + head_h}" x2="{width - pad}" '
             f'y2="{head_top + head_h}" stroke="{th["border"]}" stroke-width="1"/>')
    for i, ic in enumerate(ICONS):
        cx = pad + cell * i + cell / 2
        o.append(f'<text x="{cx:.1f}" y="{head_top + 24}" font-size="12" '
                 f'fill="{th["text2"]}" text-anchor="middle">{xesc(ic["zh"])}</text>')

    main_cy = head_top + head_h + row_h / 2
    for i, ic in enumerate(ICONS):
        gx = pad + cell * i + (cell - glyph) / 2
        gy = main_cy - glyph / 2
        o.append(f'<svg x="{gx:.1f}" y="{gy:.1f}" width="{glyph}" height="{glyph}" '
                 f'viewBox="0 0 24 24" fill="none">{glyph_elements(ic, style)}</svg>')

    o.append(f'<text x="{pad}" y="{ref_label_y}" font-size="11" fill="{th["text3"]}">'
             f'真实尺寸参照 24px</text>')
    ref_cy = ref_label_y + 26
    for i, ic in enumerate(ICONS):
        gx = pad + cell * i + (cell - 24) / 2
        gy = ref_cy - 12
        o.append(f'<svg x="{gx:.1f}" y="{gy:.1f}" width="24" height="24" '
                 f'viewBox="0 0 24 24" fill="none">{glyph_elements(ic, style)}</svg>')
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
    """读取 PNG IHDR：宽 / 高 / 位深 / 颜色类型（6 = RGBA 有透明通道）"""
    with open(path, "rb") as f:
        head = f.read(33)
    if head[:8] != b"\x89PNG\r\n\x1a\n":
        return None
    w, h = struct.unpack(">II", head[16:24])
    return w, h, head[24], head[25]


def rasterize(svg_text: str, out_png: str, width: int, height: int,
              scale: int = 2, chrome: str = "") -> bool:
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
    ap.add_argument("--skip-strips", action="store_true", help="只出 3 张矩阵图")
    ap.add_argument("--export-style", default="", help="导出该风格 12 个独立 24x24 SVG")
    ap.add_argument("--check-only", action="store_true",
                    help="不渲染，仅校验已产出的 PNG（尺寸/透明通道）")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    root = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    out_dir = args.out or os.path.join(root, "文档", "图标设计")
    os.makedirs(out_dir, exist_ok=True)

    chrome = "" if (args.svg_only or args.check_only) else find_chrome()
    if not (args.svg_only or args.check_only) and not chrome:
        print("FATAL: 未找到 Chrome/Edge，请设置 CHROME_BIN 或改用 --svg-only")
        return 2

    jobs = []   # (basename, svg_text, w, h)

    jobs.append(("sheet_all",) + grid_svg(
        STYLES, glyph=42, row_h=98, label_w=250, cell=78, head_h=40,
        title="Panomint 图标风格样图",
        subtitle="12 图标 x 10 风格 @42px · 24 网格 · 扁平 2D 矢量 · 彩色描边 · 透明底无阴影",
        footer="Panomint · 图标设计样图 · 生成脚本 scripts/gen_icon_sheets.py"))

    jobs.append(("sheet_true24",) + matrix_svg(
        STYLES, glyph=24, row_h=46, label_w=120, cell=118, head_h=52,
        title="真实尺寸对照（24px）",
        subtitle="按界面实际显示尺寸渲染 · 一行一个图标、一列一种风格 · 用于判断小尺寸下的可读性",
        footer="Panomint · 图标设计样图 · 生成脚本 scripts/gen_icon_sheets.py"))

    jobs.append(("sheet_dark",) + grid_svg(
        STYLES, glyph=36, row_h=80, label_w=210, cell=70, head_h=36,
        title="暗色页适配预览（player / share）",
        subtitle="36px · 底色 #14181d · 各色按 HSL 提亮 58% 模拟暗色 token 换色（同色直出会不可见）",
        footer="Panomint · 图标设计样图 · 暗色底为补充预览，主交付为透明底 PNG",
        theme=THEME_DARK, bg="#14181d", compact=True, darken=0.58))

    if not args.skip_strips:
        for st in STYLES:
            jobs.append(("style_" + st["key"],) + strip_svg(st))

    # 有意铺底色的图（暗色页预览）：不参与「必须透明」断言。
    # 注意必须按文件名集合判断——若沿用 jobs 循环的 name 变量，
    # 它会是上一次迭代的残留值，导致豁免失效（本脚本踩过此坑）。
    opaque_ok = {os.path.join(out_dir, n + ".png") for n in ("sheet_dark",)}

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

    if args.export_style:
        key = args.export_style
        matches = [s for s in STYLES if s["key"] == key or s["key"].startswith(key)]
        if len(matches) != 1:
            print("FATAL: --export-style 匹配到 %d 套：%s"
                  % (len(matches), [s["key"] for s in matches]))
            return 2
        st = matches[0]
        d = os.path.join(out_dir, "export_" + st["key"])
        os.makedirs(d, exist_ok=True)
        for ic in ICONS:
            with open(os.path.join(d, ic["key"] + ".svg"), "w", encoding="utf-8") as f:
                f.write(glyph_svg(ic, st, 24))
        print("  导出目录:", d, "(12 个 24x24 SVG)")

    print("\n=== SVG (%d) ===" % len(made_svg))
    for p in made_svg:
        print("  %-52s %8d B" % (os.path.basename(p), os.path.getsize(p)))

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
            transparent = (ctype == 6)
            need_alpha = p not in opaque_ok
            print("  %-52s %dx%d %s %9d B"
                  % (os.path.basename(p), w, h,
                     "RGBA" if transparent else "RGB(铺底)",
                     os.path.getsize(p)))
            if need_alpha and not transparent:
                bad.append(p)
        print("\nRESULT:", "ALL-OK" if not bad else "FAILED=%s" % bad)
        return 0 if not bad else 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
