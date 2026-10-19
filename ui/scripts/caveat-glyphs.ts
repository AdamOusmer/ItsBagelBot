// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const LATIN_END = 0x250;
const LOCALES = join(import.meta.dir, "../../locales");

const SOURCES: Array<[file: string, path: string[]]> = [
  ["console/public.json", ["footer", "signoffSub"]],
  ["website/footer.json", ["signoffSub"]],
  ["website/letter.json", ["signature"]],
];

export function caveatStrings(): string[] {
  const strings: string[] = [];
  for (const locale of readdirSync(LOCALES, { withFileTypes: true })) {
    if (!locale.isDirectory()) continue;
    for (const [file, path] of SOURCES) {
      let node: unknown = JSON.parse(readFileSync(join(LOCALES, locale.name, file), "utf8"));
      for (const key of path) node = (node as Record<string, unknown>)[key];
      if (typeof node !== "string") throw new Error(`${locale.name}/${file}: ${path.join(".")} is not a string`);
      strings.push(node);
    }
  }
  return strings;
}

export function caveatGlyphs(): string {
  const chars = new Set<string>();
  for (const text of caveatStrings()) for (const char of text) if (!/\s/.test(char) && char.codePointAt(0)! < LATIN_END) chars.add(char);
  return [...chars].sort().join("");
}

if (import.meta.main) process.stdout.write(`${caveatGlyphs()}\n`);
