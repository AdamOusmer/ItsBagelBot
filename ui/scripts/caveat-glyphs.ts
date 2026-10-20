// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// Regenerate ui/fonts/caveat-latin.woff2 from the Google Fonts original (fonttools 4.67.0, run from the repo root):
// uvx --from 'fonttools[woff]' pyftsubset caveat-latin.woff2 --text-file=ui/fonts/caveat-glyphs.txt --unicodes=U+0020,U+00A0 --layout-features='*' --flavor=woff2 --output-file=ui/fonts/caveat-latin.woff2
const LATIN_END = 0x250;
const LOCALES = join(import.meta.dir, "../../locales");

const SOURCES: Array<[file: string, path: string[]]> = [
  ["console/public.json", ["footer", "signoffSub"]],
  ["website/footer.json", ["signoffSub"]],
  ["website/letter.json", ["signature"]],
];

function readString(locale: string, [file, path]: (typeof SOURCES)[number]): string {
  const root: unknown = JSON.parse(readFileSync(join(LOCALES, locale, file), "utf8"));
  const value = path.reduce<unknown>((node, key) => (node as Record<string, unknown>)[key], root);
  if (typeof value !== "string") throw new Error(`${locale}/${file}: ${path.join(".")} is not a string`);
  return value;
}

export function caveatStrings(): string[] {
  const locales = readdirSync(LOCALES, { withFileTypes: true }).filter((entry) => entry.isDirectory());
  return locales.flatMap((locale) => SOURCES.map((source) => readString(locale.name, source)));
}

export function caveatGlyphs(): string {
  const chars = new Set<string>();
  for (const text of caveatStrings()) for (const char of text) if (!/\s/.test(char) && char.codePointAt(0)! < LATIN_END) chars.add(char);
  return [...chars].sort().join("");
}

if (import.meta.main) process.stdout.write(`${caveatGlyphs()}\n`);
