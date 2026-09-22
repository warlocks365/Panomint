#!/usr/bin/env python3
"""高信号筛查：.vue 模板事件绑定中的裸标识符引用/调用是否在文件内有定义。

只查两类形态（本次事故精确形态）：
  @event="foo"          裸引用（如 @keydown.enter="loadTags"）
  @event="foo(...)"     裸调用（如 @click="onCreate()"）
跳过大写开头（组件）、$内置、显式取成员（obj.foo）与带字面量的绑定。
"""
import os
import re
import sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else "src/frontend/src"

HANDLER = re.compile(r'@[\w.]+="([A-Za-z_$][\w$]*)(\s*\()?')
KNOWN = {"true", "false", "null", "undefined"}


def defined(text, ident):
    e = re.escape(ident)
    pats = [
        r"\bfunction\s+" + e + r"\b",
        r"\b(?:const|let|var)\s+" + e + r"\b",
        r"\b" + e + r"\s*=\s*(?:\(|[\w'\"\[{])",   # 赋值/箭头/方法简写前的等号
        r"\b" + e + r"\s*\([^)]*\)\s*\{",            # 方法简写 ident(...) {
        r"\b(?:const|let|var)\s*\{[^}]*\b" + e + r"\b[^}]*\}",  # 解构定义
        r"import\s[^;\n]*\b" + e + r"\b",
        r"\b" + e + r"\s*,",                          # import 解构/import 列表
        r"\(\s*" + e + r"\b",                         # 解构参数 ({ x } = ...)
    ]
    return any(re.search(p, text) for p in pats)


def main():
    out = []
    for dirpath, _dirs, files in os.walk(ROOT):
        for fn in files:
            if not fn.endswith(".vue"):
                continue
            p = os.path.join(dirpath, fn)
            with open(p, encoding="utf-8", errors="replace") as f:
                text = f.read()
            for m in HANDLER.finditer(text):
                ident, paren = m.group(1), m.group(2)
                if ident in KNOWN or ident.startswith("$") or ident[:1].isupper():
                    continue
                if not defined(text, ident):
                    out.append("%s: %s%s" % (p, ident, "(" if paren else ""))
    print("\n".join(out) if out else "(无命中)")
    print("---")
    print("命中 %d 处裸标识符绑定" % len(out))


if __name__ == "__main__":
    main()
