// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { caveatGlyphs } from "../scripts/caveat-glyphs";

test("the handwritten face ships every glyph the locale strings render in it", () => {
  const shipped = readFileSync(new URL("../fonts/caveat-glyphs.txt", import.meta.url), "utf8").trim();
  expect(shipped).toBe(caveatGlyphs());
});
