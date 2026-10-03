#!/usr/bin/env python3
"""Frontend dead-code scan: file-level references + api-layer dead exports."""
import os
import re

SRC = 'src'
files = []
for root, dirs, fs in os.walk(SRC):
    dirs[:] = [d for d in dirs if d not in ('node_modules', 'dist')]
    for f in fs:
        if f.endswith(('.vue', '.js')):
            files.append(os.path.join(root, f).replace('\\', '/'))

all_src = {}
for f in files:
    all_src[f] = open(f, encoding='utf-8', errors='replace').read()

# 1) file-level reference count
unreferenced = []
entries = {'src/main.js', 'src/App.vue', 'src/router/index.js'}
for f in files:
    base = os.path.splitext(os.path.basename(f))[0]
    if f in entries or base == 'index':
        continue
    refs = 0
    for f2, c2 in all_src.items():
        if f2 == f:
            continue
        if re.search(r"['\"/]%s(\.vue|\.js|['\"])" % re.escape(base), c2):
            refs += 1
    if refs == 0:
        unreferenced.append(f)

print('=== 文件级冗余候选（0 引用）===')
for f in unreferenced:
    print('  ', f)

# 2) api-layer dead exports
print('\n=== api 层死导出候选 ===')
api_files = [f for f in files if '/api/' in f]
dead_exports = []
for f in api_files:
    for m in re.finditer(r'export (?:async )?function (\w+)|export const (\w+)', all_src[f]):
        name = m.group(1) or m.group(2)
        uses = 0
        for f2, c2 in all_src.items():
            if f2 == f:
                continue
            uses += len(re.findall(r'\b%s\b' % re.escape(name), c2))
        if uses == 0:
            dead_exports.append('%s :: %s' % (f, name))
for d in dead_exports:
    print('  ', d)

# 3) stores 死导出（同法）
print('\n=== stores 层死导出候选 ===')
store_files = [f for f in files if '/stores/' in f]
dead_stores = []
for f in store_files:
    for m in re.finditer(r'export (?:async )?function (\w+)|export const (\w+)', all_src[f]):
        name = m.group(1) or m.group(2)
        uses = 0
        for f2, c2 in all_src.items():
            if f2 == f:
                continue
            uses += len(re.findall(r'\b%s\b' % re.escape(name), c2))
        if uses == 0:
            dead_stores.append('%s :: %s' % (f, name))
for d in dead_stores:
    print('  ', d)

print('\n=== 概览 ===')
print('文件总数:', len(files), '| 冗余候选:', len(unreferenced), '| api 死导出:', len(dead_exports), '| stores 死导出:', len(dead_stores))
