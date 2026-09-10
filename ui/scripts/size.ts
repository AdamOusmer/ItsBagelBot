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
    //
    // RAISED 2026-09-09 to 1400: 851 -> 1124 B gzip when the field stopped
    // owning its requestAnimationFrame loop and its inline reduced-motion
    // matchMedia call and started importing lib/raf-loop (345 B measured
    // standalone) and lib/motion-query (225 B). 1124 + 150 B linux/x64 delta
    // = 1274, +10% -> 1400.
    //
    // The row grew and the PAGE shrank, which is the one case where a raised
    // budget is not drift. This gate builds a synthetic single-entry consumer,
    // so every shared module the entry pulls in is charged to it in full. In a
    // real page the scheduler is imported once and shared by the mote field,
    // the smooth scroll, the magnetic action and (from PR7) the cursor; the
    // marketing home page used to ship four private rAF loops and two
    // matchMedia helpers. Watch this row for growth in the field's OWN
    // physics: ~570 B of the 1124 is now shared modules.
    name: "light-field",
    budget: 1400,
    external: [],
    source: `import { field } from "../../lib/light-field";
             globalThis.x = field;`,
  },
  {
    // The custom cursor: dot, lerping ring, box morph, opt-out attribute. The
    // heaviest thing in lib/ after the mote field, and the one most likely to
    // grow by accretion — every "and when you hover a video it should…" idea
    // lands here.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 1364 B gzip. That includes
    // motion-query (222 B) and raf-loop (341 B) bundled in, so the cursor's
    // OWN code is ~800 B — roughly the two 200-line components it replaces,
    // minus one of them. Budget 1700: 1364 + 150 B for CI's linux/x64 gzip
    // delta = 1514, +10% -> 1665, rounded up.
    //
    // The first number written here was 1341 against a 1650 budget, measured
    // before the frame loop was split into hovered()/paint()/tick() to clear
    // a CodeScene cyclomatic-complexity finding. Splitting one closure into
    // three cost 23 B gzip, which is what a named function and two call sites
    // weigh; the budget moves with it rather than the code moving back.
    //
    // As with light-field, the shared modules are charged to this row in full
    // because the gate builds a synthetic single-entry consumer. On a real page
    // the cursor, the mote field and the smooth scroll share one copy of each.
    name: "cursor-engine",
    budget: 1700,
    external: [],
    source: `import { mountCursor } from "../../lib/cursor-engine";
             globalThis.x = mountCursor;`,
  },
  {
    // Scroll reveal. Small and expected to stay small: an IntersectionObserver,
    // a class name and an above-the-fold check. If this row moves, what moved
    // is almost certainly a stagger scheduler or a per-element options parser,
    // and both belong in the CSS contract instead.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 639 B gzip, of which
    // 222 B is motion-query — the reduced-motion branch is the only import.
    // Budget 900: 639 + 150 B for CI's linux/x64 gzip delta = 789, +10% ->
    // 868, rounded.
    name: "reveal",
    budget: 900,
    external: [],
    source: `import { observeReveal } from "../../lib/reveal";
             globalThis.x = observeReveal;`,
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
    //
    // Re-measured 2026-09-09 after the flash constant became 1600: 139 B,
    // unchanged. Recorded rather than left silent because the number in the
    // source moved and the next person will check.
    name: "clipboard",
    budget: 320,
    external: [],
    source: `import { copyFlash } from "../../lib/clipboard";
             globalThis.x = copyFlash;`,
  },
  {
    // The two media queries every motion decision is made from. The floor of
    // the motion stack: raf-loop, lenis, the cursor and the mote field all
    // import it, so this row is charged into three of the rows below and above
    // as well. It should never move again — it is two matchMedia calls and a
    // server-side stub.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 225 B gzip. Budget 420:
    // 225 + 150 B for CI's linux/x64 gzip delta = 375, +10% -> 412, rounded.
    name: "motion-query",
    budget: 420,
    external: [],
    source: `import { prefersReducedMotion, finePointer } from "../../lib/motion-query";
             globalThis.x = [prefersReducedMotion, finePointer];`,
  },
  {
    // The single page-wide rAF scheduler. Budgeted tightly on purpose: it is
    // shared by every animated entry in this package, so anything that lands
    // here is paid for by all of them at once. If this row jumps, the thing
    // that grew is almost certainly a convenience (a priority queue, an FPS
    // cap, per-subscriber timing) that belongs in the engine that wanted it.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 345 B gzip. That is the
    // loop, the two Sets and the visibilitychange listener alone — the
    // scheduler imports nothing at all, motion-query included, and that is
    // deliberate: whether an effect
    // may run is the effect's decision, not the clock's. Budget 550:
    // 345 + 150 B for CI's linux/x64 gzip delta = 495, +10% -> 545, rounded.
    name: "raf-loop",
    budget: 550,
    external: [],
    source: `import { subscribe, wake } from "../../lib/raf-loop";
             globalThis.x = [subscribe, wake];`,
  },
  {
    // Smooth scroll. `lenis` itself is EXTERNAL here, which is the only row in
    // this file that externalises anything, and the reason is that measuring it
    // bundled would measure the vendor library (~20 KB) and drown the thing
    // this gate is actually watching: the wrapper. The wrapper is what can grow
    // by accident — an easing table, a scroll-restoration cache, a per-route
    // options map. Consumers pay for lenis once whatever this row says.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 615 B gzip, lenis external,
    // motion-query and raf-loop bundled in (570 B of the 615). Budget 850:
    // 615 + 150 B for CI's linux/x64 gzip delta = 765, +10% -> 842, rounded.
    name: "lenis",
    budget: 850,
    external: ["lenis"],
    source: `import { createSmoothScroll, getSmoothScroll } from "../../lib/lenis";
             globalThis.x = [createSmoothScroll, getSmoothScroll];`,
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
  // The `.bb-nav-link` contract: every navigation link on every surface --
  // marketing top nav, mobile menu, footer columns, console rail. The row to
  // watch for modifier creep. It replaces four per-component link styles whose
  // combined shipped weight was larger, but that is not a licence for this one
  // to grow: a fifth surface should arrive as component-property values on the
  // caller's side, which cost this file nothing, and a new MODIFIER here has to
  // justify itself against that.
  // Measured 2026-09-09: 1072 B gzip. 1072 + 150 = 1222, +10% -> 1350.
  //
  // RE-MEASURED 2026-09-09 at 1030 B, once `--cta` stopped hand-copying
  // `.bb-btn--go-solid` and started composing it (the adapters emit the button
  // classes; the fill, frame, colour and hover block deleted from this file).
  // The budget is NOT lowered to match: the row shrank by deleting a
  // duplicate, and 42 B is inside the platform delta this file already carries
  // room for. Left at 1350 so the next real modifier is measured against the
  // same line as the last one.
  { name: "elements/nav-link", budget: 1350 },
  // The button contract: seven variants, two sizes, three states, and the
  // raw-HTML pseudo fallbacks that let a call site with no adapter draw the
  // same mark. It replaces the marketing site's .bb-btn block (~120 lines),
  // the console's raw .btn block (~20) and two components' scoped copies, so
  // the number to watch is not growth but the next surface's one-off rule
  // landing here instead of behind a custom property.
  // Measured 2026-09-09 (macOS/arm64): 1181 B gzip. 1181 + 150 = 1331, +10%.
  { name: "elements/button", budget: 1470 },
  // The card contract, its head, and the atmosphere composition. Measured
  // WITH card-atmosphere.css, which card.css @imports and a bundler inlines:
  // that is what a consumer actually pays, and budgeting the two halves apart
  // would hide a move of bytes from one to the other.
  // Measured 2026-09-09 (macOS/arm64): 1170 B gzip. 1170 + 150 = 1320, +10%.
  { name: "elements/card", budget: 1460 },
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
