#!/usr/bin/env python3
"""生成 sinago.app 的图标：一根红色上涨 K 线。

只在 build-app.sh 里被调用一次，缺了也不影响运行（退回系统默认图标）。
"""
import sys
from pathlib import Path

from PIL import Image, ImageDraw

BG = (24, 26, 32)
RED = (233, 69, 69)
GREEN = (46, 176, 106)


def draw(size: int) -> Image.Image:
    # 先按 8 倍画再缩，省掉自己做抗锯齿。
    s = size * 8
    img = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, s - 1, s - 1], radius=int(s * 0.22), fill=BG)

    # 四根 K 线，最后一根拉红，一眼看出是行情工具。
    bars = [  # (低, 高, 开, 收, 颜色)
        (0.62, 0.80, 0.66, 0.76, GREEN),
        (0.50, 0.72, 0.70, 0.55, RED),
        (0.40, 0.62, 0.58, 0.45, RED),
        (0.18, 0.48, 0.44, 0.22, RED),
    ]
    n = len(bars)
    slot = s / (n + 1.2)
    body_w = slot * 0.46
    wick_w = max(2, slot * 0.09)

    for i, (lo, hi, op, cl, color) in enumerate(bars):
        cx = slot * (0.9 + i)
        d.rectangle([cx - wick_w / 2, s * lo, cx + wick_w / 2, s * hi], fill=color)
        top, bot = sorted((s * op, s * cl))
        d.rounded_rectangle(
            [cx - body_w / 2, top, cx + body_w / 2, bot],
            radius=wick_w,
            fill=color,
        )
    return img.resize((size, size), Image.LANCZOS)


def main() -> None:
    out = Path(sys.argv[1])
    draw(1024).save(out, format="ICNS")


if __name__ == "__main__":
    main()
