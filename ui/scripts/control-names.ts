// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const NAMED_CONTROLS = ['Checkbox', 'Input', 'Select', 'Slider', 'Textarea'];

const IMPORT = /import\s+(\w+)\s+from\s+['"]@bagel\/ui\/(?:svelte|astro)\/(\w+)\.(?:svelte|astro)['"]/g;
const NAME_ATTRIBUTE = /(?:^|\s)(?:aria-label|aria-labelledby|label)\s*=/;
const ID_ATTRIBUTE = /\sid=("[^"]*"|\{[^}]*\})/;
const LABELLING_TAG = /<(\/?)(Field|Label|label)\b/g;

export function importedControls(source: string): Map<string, string> {
  const locals = new Map<string, string>();
  for (const [, local, component] of source.matchAll(IMPORT)) {
    if (NAMED_CONTROLS.includes(component)) locals.set(local, component);
  }
  return locals;
}

function tagEnd(source: string, from: number): number {
  let depth = 0;
  let quote: string | null = null;
  for (let i = from; i < source.length; i++) {
    const ch = source[i];
    if (quote) {
      if (ch === quote) quote = null;
    } else if (ch === '"' || ch === "'" || (depth > 0 && ch === '`')) {
      quote = ch;
    } else if (ch === '{') {
      depth++;
    } else if (ch === '}') {
      depth--;
    } else if (ch === '>' && depth === 0) {
      return i;
    }
  }
  return source.length - 1;
}

function labelledAt(source: string, index: number): boolean {
  let open = 0;
  for (const match of source.slice(0, index).matchAll(LABELLING_TAG)) {
    open += match[1] ? -1 : 1;
  }
  return open > 0;
}

function labelledById(source: string, tag: string): boolean {
  const id = ID_ATTRIBUTE.exec(tag)?.[1];
  if (!id) return false;
  const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(`\\b(?:for|htmlFor)=${escaped}`).test(source);
}

function lineOf(source: string, index: number): number {
  return source.slice(0, index).split('\n').length;
}

export function findUnnamed(source: string): { component: string; line: number }[] {
  const locals = importedControls(source);
  const found: { component: string; line: number }[] = [];
  for (const [local, component] of locals) {
    for (const match of source.matchAll(new RegExp(`<${local}(?=[\\s/>])`, 'g'))) {
      const end = tagEnd(source, match.index);
      const tag = source.slice(match.index, end);
      const named =
        NAME_ATTRIBUTE.test(tag) ||
        (component === 'Checkbox' && !tag.endsWith('/')) ||
        labelledById(source, tag) ||
        labelledAt(source, match.index);
      if (!named) found.push({ component, line: lineOf(source, match.index) });
    }
  }
  return found;
}
