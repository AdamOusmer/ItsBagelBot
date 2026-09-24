// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// isLightSurface decides when the cursor dot blends with `difference`. The
// chain is computed styles from the element under the pointer outwards, in
// the format WebKit and Chromium serialize them.

import { expect, test } from "bun:test";
import { isLightSurface } from "../lib/cursor-engine";

const surface = (backgroundColor = "rgba(0, 0, 0, 0)", backgroundImage = "none") => ({
  backgroundColor,
  backgroundImage,
});

test("the dark page is not light", () => {
  expect(isLightSurface([surface(), surface(), surface("rgb(10, 10, 10)")])).toBe(false);
});

test("an opaque tan fill is light", () => {
  expect(isLightSurface([surface(), surface("rgb(201, 168, 124)")])).toBe(true);
});

test("the nearest opaque fill decides, not one further out", () => {
  expect(isLightSurface([surface("rgb(17, 17, 16)"), surface("rgb(240, 236, 228)")])).toBe(false);
});

test("translucent fills are looked through", () => {
  expect(isLightSurface([surface("rgba(201, 168, 124, 0.08)"), surface("rgb(10, 10, 10)")])).toBe(false);
});

test("a light gradient counts even with no fill colour", () => {
  const card = surface("rgba(0, 0, 0, 0)", "linear-gradient(135deg, rgb(240, 236, 228) 0%, rgb(220, 198, 164) 100%)");
  expect(isLightSurface([surface(), card, surface("rgb(10, 10, 10)")])).toBe(true);
});

test("a gradient of only faint stops is looked through", () => {
  const wash = surface("rgba(0, 0, 0, 0)", "radial-gradient(rgba(201, 168, 124, 0.12), rgba(0, 0, 0, 0))");
  expect(isLightSurface([wash, surface("rgb(10, 10, 10)")])).toBe(false);
});

test("an empty chain is not light", () => {
  expect(isLightSurface([])).toBe(false);
});
