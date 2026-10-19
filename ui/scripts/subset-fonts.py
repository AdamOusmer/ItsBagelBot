# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

# Narrows ui/fonts/caveat-latin.woff2 in place to the glyphs in ui/fonts/caveat-glyphs.txt (bun ui/scripts/caveat-glyphs.ts
# prints them from the locale strings). It cannot widen: start from the Google Fonts original for that.
# Run: uvx --with fonttools --with brotli python ui/scripts/subset-fonts.py

from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont

FONTS = Path(__file__).resolve().parent.parent / "fonts"
SPACES = {0x20, 0xA0}


def main():
    path = FONTS / "caveat-latin.woff2"
    glyphs = (FONTS / "caveat-glyphs.txt").read_text(encoding="utf-8")
    codepoints = sorted({ord(char) for char in glyphs if not char.isspace()} | SPACES)
    font = TTFont(path)
    options = subset.Options(layout_features=["*"], glyph_names=False, notdef_outline=True, flavor="woff2")
    subsetter = subset.Subsetter(options)
    subsetter.populate(unicodes=codepoints)
    subsetter.subset(font)
    font.flavor = "woff2"
    font.save(path)
    print(path.name, path.stat().st_size)


main()
