// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// A keyframe may animate transform and opacity, nothing else, unless it is
// listed in ALLOWED with the reason it cannot hurt. See styles/motion.css for
// why: a paint property animated every frame costs WebKit a pool of backing
// surfaces, measured at +150-250 MB on a phone.
//
// Usage: bun scripts/assert-compositor-motion.mjs [dir ...]   (default: styles/)
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const COMPOSITED = new Set([
  'transform', 'translate', 'scale', 'rotate', 'opacity',
  'animation-timing-function', 'offset-distance',
]);

// name -> why a paint property here is acceptable.
const ALLOWED = new Map([
  ['bb-draw', 'unused .bb-drawline; width by design, see nav-link.css'],
  ['bb-error-rise', 'one-shot entrance'],
  ['bb-code-in', 'one-shot entrance'],
  ['bb-page-hero-in', 'one-shot entrance'],
  ['drawLeaf', 'one-shot draw, iteration count 1'],
  ['draw-circle', 'one-shot draw, fill-mode forwards'],
  ['draw-check', 'one-shot draw, fill-mode forwards'],
  ['pop', 'one-shot entrance'],
  ['sheen', 'one-shot sweep, fill-mode both'],
  ['qwType', 'steps(18): repaints once per character, not per frame'],
  ['gamesTypeMcBw', 'steps(): repaints once per character, not per frame'],
  ['gamesTypeMcMcsr', 'steps(): repaints once per character, not per frame'],
  ['gamesTypeVal', 'steps(): repaints once per character, not per frame'],
  ['gamesTypeCr', 'steps(): repaints once per character, not per frame'],
]);

const EXTENSIONS = ['.css', '.astro', '.svelte'];

function walk(dir) {
  return readdirSync(dir)
    .sort()
    .flatMap((name) => {
      if (name === 'node_modules' || name.startsWith('.')) return [];
      const path = join(dir, name);
      if (statSync(path).isDirectory()) return walk(path);
      return EXTENSIONS.some((ext) => name.endsWith(ext)) ? [path] : [];
    });
}

/** Every `@keyframes name { ... }` in `css`, with the properties it animates. */
export function keyframeProperties(css) {
  const found = [];
  const opener = /@keyframes\s+([\w-]+)\s*\{/g;
  let match;
  while ((match = opener.exec(css))) {
    let depth = 1;
    let i = opener.lastIndex;
    while (depth > 0 && i < css.length) {
      if (css[i] === '{') depth++;
      else if (css[i] === '}') depth--;
      i++;
    }
    const body = css.slice(opener.lastIndex, i - 1).replace(/\/\*[\s\S]*?\*\//g, '');
    const properties = new Set(
      [...body.matchAll(/(?:^|[{;])\s*([a-z-]+)\s*:/g)].map((m) => m[1]),
    );
    found.push({ name: match[1], properties: [...properties] });
    opener.lastIndex = i;
  }
  return found;
}

export function paintKeyframes(css) {
  return keyframeProperties(css)
    .map(({ name, properties }) => ({ name, paint: properties.filter((p) => !COMPOSITED.has(p)) }))
    .filter(({ name, paint }) => paint.length > 0 && !ALLOWED.has(name));
}

if (import.meta.main) {
  const roots = process.argv.slice(2);
  const dirs = roots.length ? roots : [new URL('../styles/', import.meta.url).pathname];
  const bad = dirs.flatMap((dir) =>
    walk(dir).flatMap((path) =>
      paintKeyframes(readFileSync(path, 'utf8')).map(
        ({ name, paint }) => `${relative(process.cwd(), path)}: @keyframes ${name} animates ${paint.join(', ')}`,
      ),
    ),
  );
  if (bad.length) {
    console.error(`Keyframes animating paint properties (transform/opacity only, see ui/styles/motion.css):\n  ${bad.join('\n  ')}`);
    process.exit(1);
  }
  console.log('keyframes animate transform/opacity only');
}
