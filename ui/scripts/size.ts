/**
 * Bundle size gate for @bagel/ui: the same shape as web/kit/scripts/size.ts
 * (itself the bolota pattern), applied to the entries this library asks its
 * consumers to bundle rather than externalise.
 *
 * It matters more here than in kit. Every ui entry is imported through a
 * subpath export, and the whole argument for subpaths over one barrel is that
 * a page pulling `@bagel/ui/lib/clipboard` must not also pay for the mote
 * field. A row per entry is what proves that separation is still real; the day
 * someone adds a convenience re-export between two of them, the smaller row
 * jumps and the gate says so.
 *
 * Measured through synthetic consumers rather than by building a module
 * directly: an entry with no importer tree-shakes to nothing, which reports a
 * flattering number no real app ever sees.
 *
 * Budgets are per entry, gzip bytes, and deliberately carry headroom over the
 * measurement. Two reasons, and the second is why the headroom is not a flat
 * percentage: gzip output differs by platform (this repo's CI runner is
 * linux/x64 and gzips the same bytes ~100-150 B larger than the macOS/arm64
 * these were measured on — bolota once failed CI on that delta with no source
 * change), and on rows this small 150 B is a larger share than any sane
 * percentage. So each budget below is measured + ~150 B for the platform delta,
 * and THEN ~10% of room on top.
 *
 * Raising one is allowed and expected when a feature genuinely costs bytes:
 * record what grew, the measured figure, and the room left, in a comment next
 * to the number, in the same commit.
 */

import { mkdirSync, rmSync, writeFileSync } from "node:fs";

const DIR = new URL("./.fixtures/", import.meta.url).pathname;

const ENTRIES: {
  name: string;
  budget: number;
  external: string[];
  source: string;
}[] = [
  {
    // The drifting mote field: the physics both the marketing hero canvases
    // and the console's LightField.svelte run. Client-facing on every surface
    // that draws one, and the largest thing in lib/ by an order of magnitude,
    // so this is the row to watch.
    //
    // Initial measurement 2026-09-09 (macOS/arm64), at the move out of
    // web/kit into the standalone library: 851 B gzip, unchanged bytes — the
    // file moved, it did not grow. Budget 1100, not the 934 a flat +10% would
    // give: 851 + 150 B for CI's linux/x64 gzip delta = 1001, +10% -> 1100.
    name: "light-field",
    budget: 1100,
    external: [],
    source: `import { field } from "../../lib/light-field";
             globalThis.x = field;`,
  },
  {
    // Copy-to-clipboard with the "Copied" flash. Tiny on purpose, and budgeted
    // precisely BECAUSE it is tiny: it is the entry most likely to acquire a
    // toast import or a shared-state module some day, and this row is what
    // turns that into a failed build instead of an unnoticed 4 KB.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 139 B gzip. Budget 320,
    // not the 153 a flat +10% would give: at this size CI's ~150 B linux/x64
    // gzip delta is larger than the module (139 + 150 = 289, +10% -> 320).
    name: "clipboard",
    budget: 320,
    external: [],
    source: `import { copyFlash } from "../../lib/clipboard";
             globalThis.x = copyFlash;`,
  },
];

/* The CSS contracts. Measured directly rather than through a synthetic
 * consumer: a stylesheet has no tree-shaking to defeat, so the file IS what
 * ships, minified, once per surface bundle that imports it.
 *
 * These rows exist for a different reason than the JS ones. Every element
 * contract that lands in this library arrives by DELETING two or three
 * copies of itself from the surfaces, so the number that matters is not "did
 * this file grow" but "did it grow by more than the copies it replaced". A
 * row here turns the second half of that into a build failure: a contract
 * that quietly acquires a surface's one-off rules instead of exposing a
 * custom property for them shows up as bytes.
 *
 * Same budget arithmetic as above: measured + ~150 B for CI's linux/x64 gzip
 * delta, then ~10% of room, rounded.
 */
const CSS_ENTRIES: { name: string; budget: number }[] = [
  // Labels, tags, marks, sweeps and chips. Was ~120 lines in the marketing
  // style.css and ~110 in the console app.css; both are deleted.
  // Measured 2026-09-09 (macOS/arm64): 1336 B gzip. 1336 + 150 = 1486, +10%.
  { name: "tags", budget: 1640 },
  // The 24px/760ms/80ms entrance, both the scroll transition and the load
  // keyframe. Measured 2026-09-09: 307 B gzip. 307 + 150 = 457, +10%.
  { name: "reveal", budget: 520 },
  // Ambient orbs: shape, halo, washes and drifts, replacing four copies.
  // Measured 2026-09-09: 744 B gzip. 744 + 150 = 894, +10%.
  { name: "orbs", budget: 1000 },
  // Reduced motion, focus ring, scrollbar. The smallest of the four and the
  // one most likely to become a dumping ground for "global-ish" rules, which
  // is what this row is for. Measured 2026-09-09: 348 B gzip.
  { name: "a11y", budget: 560 },
];

let failed = false;
rmSync(DIR, { recursive: true, force: true });
mkdirSync(DIR, { recursive: true });

for (const entry of ENTRIES) {
  const file = `${DIR}${entry.name.replace(/[^a-z0-9]+/gi, "-")}.ts`;
  writeFileSync(file, entry.source);

  const build = await Bun.build({
    entrypoints: [file],
    external: entry.external,
    minify: true,
    target: "browser",
  });
  if (!build.success) {
    console.error(`✗ ${entry.name}: build failed`);
    for (const log of build.logs) console.error(String(log));
    failed = true;
    continue;
  }

  const js = await build.outputs[0].arrayBuffer();
  const gz = Bun.gzipSync(new Uint8Array(js)).byteLength;
  const ok = gz <= entry.budget;
  const room = entry.budget - gz;
  console.log(
    `${ok ? "✓" : "✗"} ${entry.name}: ${gz} B gzip (budget ${entry.budget}, ${room >= 0 ? `${room} B room` : `${-room} B OVER`})`,
  );
  if (!ok) failed = true;
}

for (const entry of CSS_ENTRIES) {
  const build = await Bun.build({
    entrypoints: [new URL(`../styles/${entry.name}.css`, import.meta.url).pathname],
    minify: true,
  });
  if (!build.success) {
    console.error(`✗ ${entry.name}.css: build failed`);
    for (const log of build.logs) console.error(String(log));
    failed = true;
    continue;
  }

  const css = await build.outputs[0].arrayBuffer();
  const gz = Bun.gzipSync(new Uint8Array(css)).byteLength;
  const ok = gz <= entry.budget;
  const room = entry.budget - gz;
  console.log(
    `${ok ? "✓" : "✗"} ${entry.name}.css: ${gz} B gzip (budget ${entry.budget}, ${room >= 0 ? `${room} B room` : `${-room} B OVER`})`,
  );
  if (!ok) failed = true;
}

rmSync(DIR, { recursive: true, force: true });
if (failed) {
  console.error(
    "\nsize gate failed. If the growth is deliberate, raise the budget in ui/scripts/size.ts with what grew, the measured figure, and the room left, in the same commit.",
  );
  process.exit(1);
}
