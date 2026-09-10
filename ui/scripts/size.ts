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
    // RE-MEASURED 2026-09-09 at 1373 B when the hover selector's `.search`
    // became `.bb-input` (the field contract renamed the console's text-control
    // frame). Two characters, 2 B gzip; the budget does not move.
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
    // The count-up readout StatTile drives. Client-facing wherever a stat grid
    // renders, and the entry most exposed to the shared-scheduler rewrite in
    // the motion PR: it owns its own requestAnimationFrame loop today and will
    // hand that to raf-loop later, so this row is what says the swap made it
    // smaller rather than merely different.
    //
    // Initial measurement 2026-09-09 (macOS/arm64), at the move out of
    // web/kit/lib/actions.ts: 414 B gzip. Budget 620: 414 + 150 B for CI's
    // linux/x64 gzip delta = 564, +10% -> 620.
    //
    // RE-MEASURED 2026-09-09 at 471 B on the rebased stack, 149 B of room
    // left. The module is byte-identical and imports nothing, so the 57 B is
    // the gate's own floor moving, not this entry growing; recorded rather
    // than left silent so the next reader is not comparing against a number
    // this machine no longer produces. Budget unchanged.
    name: "count-up",
    budget: 620,
    external: [],
    source: `import { countUp } from "../../lib/count-up";
             globalThis.x = countUp;`,
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
  {
    // The mobile nav panel's choreography: three concurrent tweens, the SVG
    // curve, the stagger, plus the brand mark's scroll-to-top. The biggest
    // single engine in the package after the cursor, and the row that proves
    // the `motion` dependency did not have to come with it: 1901 B here
    // against ~30 KB for the smallest `motion` entry exporting `animate` and
    // `cubicBezier`, which is what the component this replaces imported.
    //
    // `lenis` is external for the same reason as the row below it -- the
    // engine reads the running scroller through lib/lenis to scroll home, and
    // bundling the vendor library would drown the 300 B of wiring that can
    // actually grow by accident.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 1901 B gzip, with tween
    // (656 B), raf-loop and motion-query charged in full by the synthetic
    // single-entry consumer. Budget 2300: 1901 + 150 B for CI's linux/x64
    // gzip delta = 2051, +10% -> 2256, rounded.
    name: "nav-menu",
    budget: 2300,
    external: ["lenis"],
    source: `import { mountTopDownMenu, mountHomeLogo } from "../../lib/nav-menu";
             globalThis.x = [mountTopDownMenu, mountHomeLogo];`,
  },
  {
    // One driven number on a cubic-bezier, on the shared scheduler. Budgeted
    // tightly BECAUSE it is the obvious place to bolt on the next thing an
    // animation wants: springs, keyframes, a timeline, an interpolator for
    // colours. Every one of those belongs to the engine that needs it, and
    // this row is what says so.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 656 B gzip, of which
    // ~345 B is raf-loop bundled in -- the tween's own code, including the
    // Newton-Raphson solve, is ~310 B. Budget 950: 656 + 150 = 806, +10% ->
    // 887, rounded.
    name: "tween",
    budget: 950,
    external: [],
    source: `import { tween, bezier } from "../../lib/tween";
             globalThis.x = [tween, bezier];`,
  },
  {
    // The rail's measured highlight: read two offsets, write two custom
    // properties, re-measure under a ResizeObserver. It should never move
    // again; if it does, what arrived is almost certainly per-row logic that
    // belongs in the CSS contract.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 329 B gzip, importing
    // nothing at all. Budget 560: 329 + 150 = 479, +10% -> 527, rounded.
    name: "rail-glide",
    budget: 560,
    external: [],
    source: `import { mountGlide } from "../../lib/rail-glide";
             globalThis.x = mountGlide;`,
  },
  {
    // One shared interval for every clock on the page. Tiny, and budgeted
    // precisely because the temptation here is a formatter cache or a
    // timezone map, neither of which is this element's business.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 254 B gzip. Budget 480:
    // 254 + 150 = 404, +10% -> 444, rounded.
    name: "clock",
    budget: 480,
    external: [],
    source: `import { mountClock } from "../../lib/clock";
             globalThis.x = mountClock;`,
  },
  {
    // The dock's folding arithmetic: six pure functions, no DOM. The row is
    // here rather than folded into the adapter's cost because these are the
    // functions a static surface calls at BUILD time and ships nothing for --
    // if this row starts growing, the arithmetic has acquired state.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 428 B gzip. Budget 700:
    // 428 + 150 = 578, +10% -> 636, rounded up.
    name: "dock-groups",
    budget: 700,
    external: [],
    source: `import * as dock from "../../lib/dock-groups";
             globalThis.x = dock;`,
  },
  {
    // A hashchange listener and a class toggle. The whole point of this
    // module is that it is NOT a scrollspy, and a scrollspy is what would
    // make this row jump: an IntersectionObserver, a threshold table and a
    // "which section wins" tiebreak.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 255 B gzip. Budget 480:
    // 255 + 150 = 405, +10% -> 446, rounded.
    name: "hash-active",
    budget: 480,
    external: [],
    source: `import { mountHashActive } from "../../lib/hash-active";
             globalThis.x = mountHashActive;`,
  },
  {
    // The generated icon set: 33 glyph bodies as string constants. The
    // largest module in lib/ by bytes, and the one whose growth is a design
    // decision rather than an engineering one -- every name added here is
    // paid for by every surface that imports the map.
    //
    // It is measured with the whole map reachable (`globalThis.x = icons`),
    // which is the worst case and not the common one: a surface that names
    // its glyphs statically lets the bundler drop the rest. The row exists to
    // keep the worst case visible.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 2640 B gzip for 33
    // icons, replacing two generated files (26 + 9 names, one `x` in common
    // under two different glyphs) that shipped separately to the console and
    // the marketing site. Budget 3200: 2640 + 150 = 2790, +10% -> 3069,
    // rounded.
    name: "icons",
    budget: 3200,
    external: [],
    source: `import { icons } from "../../lib/icons";
             globalThis.x = icons;`,
  },
  {
    // Decode-on-view text. Charged the shared frame loop and the motion query
    // in full (571 B of the row), which is the gate working as designed: this
    // is what a page that imports ONLY the decode engine pays.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 990 B gzip, at the
    // move out of web/marketing/src/script/decode.js. The scramble table is
    // 46 characters of the total; the rest is the observer and the tick.
    // Budget: 990 + 150 B for CI's linux/x64 gzip delta = 1140, +10% -> 1260.
    name: "decode",
    budget: 1260,
    external: [],
    source: `import { observeDecode } from "../../lib/decode";
             globalThis.x = observeDecode;`,
  },
  {
    // The reading bar's driver: one passive scroll listener, one resize
    // listener, and a frame subscription per burst. Small, and it has to stay
    // small — it runs on every marketing page, on every scroll.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 521 B gzip, of which
    // raf-loop is 346. Budget: 521 + 150 B for CI's linux/x64 gzip
    // delta = 671, +10% -> 740.
    name: "reading-progress",
    budget: 740,
    external: [],
    source: `import { mountReadingProgress } from "../../lib/reading-progress";
             globalThis.x = mountReadingProgress;`,
  },
  {
    // The overlay stack: ref-counted scroll lock, background inert, z-order,
    // topmost-only Escape, portal and focus trap. Every modal surface in the
    // console pulls it in, so it is the row that decides what a dialog costs
    // before it has drawn anything.
    //
    // It imports NOTHING, deliberately, and that is worth a line here because
    // the obvious tidy-up would break it: the scroll lock reaches the smooth
    // scroller through `window.__lenis` rather than through lib/lenis, because
    // that module statically imports lenis (~20 KB) and this file is on every
    // dialog's path.
    //
    // Initial measurement 2026-09-09 (macOS/arm64): 751 B gzip, at the
    // move out of web/kit. Budget: 751 + 150 B for CI's linux/x64 gzip
    // delta = 901, +10% -> 1000.
    name: "overlay-stack",
    budget: 1000,
    external: [],
    source: `import { pushOverlay, portal, trapFocus } from "../../lib/overlay-stack";
             globalThis.x = [pushOverlay, portal, trapFocus];`,
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
  // The icon element: two declarations. It is a row rather than nothing
  // because "the icon needs one more rule" is how a 400-byte utility grows out
  // of a 60-byte contract. Measured 2026-09-09: 83 B gzip. 83 + 150 = 233,
  // +10% -> 256, rounded.
  { name: "elements/icon", budget: 280 },
  // The call sign, replacing four hand-written copies (rail 30px, strip 26px,
  // marketing bar 26px, footer 55px). Measured 2026-09-09: 592 B gzip.
  // 592 + 150 = 742, +10% -> 816, rounded.
  { name: "elements/brand-mark", budget: 900 },
  // The site nav family: bar, hamburger, mobile panel, social rail, locale
  // switch, nav group. Six blocks in one file because they are one surface;
  // it replaces ~600 lines of scoped styles across five components.
  // Measured 2026-09-09: 1943 B gzip. 1943 + 150 = 2093, +10% -> 2302.
  //
  // RE-MEASURED 2026-09-10 at 2047 B on the rebased stack, 353 B of room left.
  // nav.css is byte-identical to the 1943 measurement (diffed against the
  // pre-rebase blob), so the 104 B is the gate's own floor moving under a
  // different Bun minifier, not this contract growing. Recorded rather than
  // left silent because the next reader will run this and get 2047, not 1943.
  // The same run moved nav-menu 1901 -> 1900, tween 656 -> 655, clock
  // 254 -> 253 and dock-groups 428 -> 426; those are inside the noise and do
  // not each get a paragraph of their own. Budget unchanged: 2302 was the
  // arithmetic and 2400 the rounding, and 2047 is under both.
  { name: "elements/nav", budget: 2400 },
  // The sign-off. Measured 2026-09-09: 791 B gzip. 791 + 150 = 941,
  // +10% -> 1035, rounded.
  { name: "elements/footer", budget: 1100 },
  // The application shell: rail, strip, dock, stage, section nav, page head,
  // toolbar, scroller. The largest contract in the package, and deliberately
  // one file -- see the note at the top of shell.css for why splitting it put
  // the same three numbers in three places each. It replaces ~1100 lines of
  // component-scoped CSS. Measured 2026-09-09: 3214 B gzip. 3214 + 150 =
  // 3364, +10% -> 3700, rounded up.
  { name: "elements/shell", budget: 3800 },
  /* The per-element contracts (styles/elements/*.css). Same arithmetic:
   * measured + ~150 B for CI's linux/x64 gzip delta, then ~10%, rounded.
   * All ten measured 2026-09-09 (macOS/arm64) at the move out of
   * web/kit/styles/console.css and the components' scoped <style> blocks.
   *
   * Read them as a set: ~3.8 KB gzip of contract against the console.css
   * rules and scoped blocks this PR deletes. An element whose row grows
   * without a call site gaining a feature is a contract that has absorbed a
   * surface's one-off instead of exposing a custom property. */
  // 135 B. Deliberately tiny: the perm ladder is @bagel/kit's, and this row
  // is what fails if it tries to come back.
  { name: "elements/badge", budget: 320 },
  // 153 B. [data-on] + :disabled only; the frame is tags.css's .bb-chip.
  { name: "elements/chip", budget: 340 },
  // 284 B.
  { name: "elements/empty-state", budget: 500 },
  // 517 B. Carries .bb-input, the frame every text control in the console
  // wears, so it is the largest of the form rows and the one to watch.
  { name: "elements/field", budget: 760 },
  // 512 B.
  { name: "elements/radio-group", budget: 760 },
  // 298 B.
  { name: "elements/search-input", budget: 520 },
  // 160 B.
  { name: "elements/segmented", budget: 360 },
  // 454 B.
  { name: "elements/skeleton", budget: 700 },
  // 703 B. The biggest, and fairly: it carries the grid, the tile, three
  // breakpoints and the entry stagger.
  { name: "elements/stat-tile", budget: 980 },
  // 562 B.
  //
  // RE-MEASURED 2026-09-10 at 687 B, 133 B of room left. The +125 is the
  // `.bb-switch-row` block: the row, label and hint that @bagel/kit's
  // MasterToggle.svelte used to carry as a scoped <style>, moved here so the
  // kit gate (web/kit/scripts/assert-ui-only-in-ui.mjs) can run unconditional.
  // The row grew and a component's scoped block of the same size went away, so
  // the page is unchanged; budget held at 820 rather than raised, because this
  // is the last thing that file owed this one.
  { name: "elements/toggle", budget: 820 },
  // The inline degraded band plus the fixed, inverted impersonation bar. It
  // replaces the page-local `.degraded` block 31 pages hand-rolled, so the
  // row to watch is a fourth tone arriving as rules instead of as three
  // `--alert-*` values on the caller.
  // Measured 2026-09-09 (macOS/arm64): 507 B gzip. 507 + 150 = 657, +10% -> 730.
  { name: "elements/alert", budget: 730 },
  // Chart chrome only: gridlines, curve, dot, tick gutter. The geometry is the
  // adapter's, which is why this row is small and must stay small.
  // Measured 2026-09-09: 203 B gzip. 203 + 150 = 353, +10% -> 390.
  { name: "elements/area-series", budget: 390 },
  // Composition over orbs.css: which three orbs, where, how bright. If this
  // row ever approaches the orbs row, the composition has started redefining
  // the shape.
  // Measured 2026-09-09: 257 B gzip. 257 + 150 = 407, +10% -> 450.
  { name: "elements/aurora", budget: 450 },
  // Placement of the shell's ambient pair; shape and wash are orbs.css.
  // Measured 2026-09-09: 179 B gzip. 179 + 150 = 329, +10% -> 370.
  { name: "elements/bg-orbs", budget: 370 },
  // Corner brackets, wordmark, and the shared `--ornament-inline` inset the
  // social rail aligns to.
  // Measured 2026-09-09: 446 B gzip. 446 + 150 = 596, +10% -> 660.
  { name: "elements/brackets", budget: 660 },
  // A card preset. The plate is copied from Card.svelte until card.css lands,
  // so this row is expected to SHRINK on the button+card re-stack, not grow.
  // Measured 2026-09-09: 161 B gzip. 161 + 150 = 311, +10% -> 350.
  //
  // RE-MEASURED 2026-09-10 at 74 B: the copied card plate is gone and the file
  // is one custom property, which is what the note above predicted. Budget
  // NOT lowered to match. 350 is already the floor this arithmetic produces
  // for anything (a 74 B file is far under the ~150 B platform delta the
  // budgets carry room for), and re-cutting it would only mean re-raising it
  // the first time a deck needs a second declaration.
  { name: "elements/deck-list", budget: 350 },
  // The sticky action bar and its four status tones.
  // Measured 2026-09-09: 431 B gzip. 431 + 150 = 581, +10% -> 640.
  // Re-measured 2026-09-10 at 432 B, when the 44px touch-target selector
  // became `.bb-btn` with the Button adapter. One byte; budget unchanged.
  { name: "elements/editor-footer", budget: 640 },
  // The largest row in the package, and the one with the weakest claim to its
  // bytes: two orbit systems, an outlined display code, six staggered
  // entrances and their reduced-motion opt-out, for a page nobody wants to be
  // on. It is here rather than trimmed because it is the surface a visitor
  // meets when something has already gone wrong. Anything ADDED to it should
  // be argued against this sentence.
  // Measured 2026-09-09: 1582 B gzip. 1582 + 150 = 1732, +10% -> 1910.
  { name: "elements/error-scene", budget: 1910 },
  // The row whose structure is the accessibility fix.
  // Measured 2026-09-09: 387 B gzip. 387 + 150 = 537, +10% -> 600.
  { name: "elements/management-row", budget: 600 },
  // The centred dialog, plus the `confirm-danger` tone that belongs in
  // button.css and is parked here for the length of the stack — this row
  // shrinks when it leaves.
  // Measured 2026-09-09: 669 B gzip. 669 + 150 = 819, +10% -> 910.
  //
  // RE-MEASURED 2026-09-10 at 608 B: the `.confirm-danger` button tone this
  // file was holding for ConfirmDialog is deleted, as its own note said it
  // should be once button.css landed in the package. Budget left at 910: the
  // row shrank by giving a rule back to the contract that owns it, and 910 is
  // still the number the next real modal feature is measured against.
  { name: "elements/modal", budget: 910 },
  // Two columns, one collapse point, one margin rule.
  // Measured 2026-09-09: 225 B gzip. 225 + 150 = 375, +10% -> 420.
  { name: "elements/overview-grid", budget: 420 },
  // The inner-page masthead: glow, measure, three staggered entrances. Ten
  // pages render it and none of them ship a masthead of their own any more.
  // Measured 2026-09-09: 839 B gzip. 839 + 150 = 989, +10% -> 1090.
  { name: "elements/page-hero", budget: 1090 },
  // A 2px track and a scaleX fill.
  // Measured 2026-09-09: 251 B gzip. 251 + 150 = 401, +10% -> 450.
  { name: "elements/reading-progress", budget: 450 },
  // One rule. Everything else the save indicator draws is the tags
  // vocabulary, and this row is the proof: the day it stops being ~100 B,
  // someone has re-created the four bespoke pulse animations it replaced.
  // Measured 2026-09-09: 95 B gzip. 95 + 150 = 245, +10% -> 270.
  { name: "elements/save-status", budget: 270 },
  // Eyebrow, badge line, title.
  // Measured 2026-09-09: 358 B gzip. 358 + 150 = 508, +10% -> 560.
  { name: "elements/section-heading", budget: 560 },
  // Both inspector forms, docked and sheet, plus the sheet entrance.
  // Measured 2026-09-09: 733 B gzip. 733 + 150 = 883, +10% -> 980.
  // Re-measured 2026-09-10 at 728 B, when `--docked` stopped hand-copying the
  // card plate and started composing `.bb-card` (three declarations out, one
  // custom property in). 5 B; budget unchanged. Note that the composition
  // means a consumer of the docked form now also pays card.css, which is a row
  // of its own above and not a cost this row hides.
  { name: "elements/surface", budget: 980 },
  // The per-glyph roll, the ember rail and the glint, in three pointer
  // stories. Expensive for a text link, which is exactly why the nav and the
  // footer render `.bb-nav-link` instead and this one is reserved for prose.
  // Measured 2026-09-09: 1018 B gzip. 1018 + 150 = 1168, +10% -> 1290.
  { name: "elements/text-link", budget: 1290 },
  // The stack, three tones, the undo control and the entrance.
  // Measured 2026-09-09: 714 B gzip. 714 + 150 = 864, +10% -> 960.
  { name: "elements/toast", budget: 960 },
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
