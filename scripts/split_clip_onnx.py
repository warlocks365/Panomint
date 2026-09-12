"""把合并的 CLIP ONNX 切成单塔子模型（Job000010）。

Chinese-CLIP 的 Xenova 导出是**合并模型**（3 输入 input_ids/pixel_values/attention_mask，
输出含 text_embeds/image_embeds）。若直接使用，每次文本查询都要白跑一遍视觉塔。
这里按输入/输出子集抽取单塔模型，避免无谓算力。

用法：python split_onnx.py <目录>
产出：<目录>/text_only.onnx、<目录>/vision_only.onnx
"""
import os
import sys

from onnx import load
from onnx.utils import extract_model


def main() -> int:
    d = sys.argv[1] if len(sys.argv) > 1 else "."
    src = os.path.join(d, "model_quantized.onnx")
    if not os.path.exists(src):
        print("缺少源模型:", src)
        return 1

    targets = [
        ("text_only.onnx", ["input_ids", "attention_mask"], ["text_embeds"]),
        ("vision_only.onnx", ["pixel_values"], ["image_embeds"]),
    ]
    for out, ins, outs in targets:
        dst = os.path.join(d, out)
        if os.path.exists(dst):
            print("已存在，跳过:", out)
            continue
        extract_model(src, dst, ins, outs)
        print("生成 %s (%.1f MB)" % (out, os.path.getsize(dst) / 1e6))

    # 校验产出模型的签名
    for out, _, _ in targets:
        m = load(os.path.join(d, out), load_external_data=False)
        g = m.graph
        print(
            "%-18s in=%s out=%s"
            % (
                out,
                [i.name for i in g.input],
                [o.name for o in g.output],
            )
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
