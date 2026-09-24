
import { mkdirSync, rmSync, writeFileSync } from "node:fs";

const DIR = new URL("./.fixtures/", import.meta.url).pathname;

const ENTRIES: {
  name: string;
  budget: number;
  external: string[];
  source: string;
}[] = [
  {
    name: "light-field",
    budget: 1400,
    external: [],
    source: `import { field } from "../../lib/light-field";
             globalThis.x = field;`,
  },
  {
    name: "cursor-engine",
    budget: 1700,
    external: [],
    source: `import { mountCursor } from "../../lib/cursor-engine";
             globalThis.x = mountCursor;`,
  },
  {
    name: "reveal",
    budget: 900,
    external: [],
    source: `import { observeReveal } from "../../lib/reveal";
             globalThis.x = observeReveal;`,
  },
  {
    name: "count-up",
    budget: 620,
    external: [],
    source: `import { countUp } from "../../lib/count-up";
             globalThis.x = countUp;`,
  },
  {
    name: "clipboard",
    budget: 700,
    external: [],
    source: `import { copyFlash } from "../../lib/clipboard";
             globalThis.x = copyFlash;`,
  },
  {
    name: "motion-query",
    budget: 420,
    external: [],
    source: `import { prefersReducedMotion, finePointer } from "../../lib/motion-query";
             globalThis.x = [prefersReducedMotion, finePointer];`,
  },
  {
    name: "raf-loop",
    budget: 550,
    external: [],
    source: `import { subscribe, wake } from "../../lib/raf-loop";
             globalThis.x = [subscribe, wake];`,
  },
  {
    name: "lenis",
    budget: 1250,
    external: ["lenis"],
    source: `import { createPaneScroll, createSmoothScroll, getSmoothScroll } from "../../lib/lenis";
             globalThis.x = [createPaneScroll, createSmoothScroll, getSmoothScroll];`,
  },
  {
    name: "nav-menu",
    budget: 2300,
    external: ["lenis"],
    source: `import { mountTopDownMenu, mountHomeLogo } from "../../lib/nav-menu";
             globalThis.x = [mountTopDownMenu, mountHomeLogo];`,
  },
  {
    name: "tween",
    budget: 950,
    external: [],
    source: `import { tween, bezier } from "../../lib/tween";
             globalThis.x = [tween, bezier];`,
  },
  {
    name: "rail-glide",
    budget: 560,
    external: [],
    source: `import { mountGlide } from "../../lib/rail-glide";
             globalThis.x = mountGlide;`,
  },
  {
    name: "clock",
    budget: 480,
    external: [],
    source: `import { mountClock } from "../../lib/clock";
             globalThis.x = mountClock;`,
  },
  {
    name: "dock-groups",
    budget: 700,
    external: [],
    source: `import * as dock from "../../lib/dock-groups";
             globalThis.x = dock;`,
  },
  {
    name: "hash-active",
    budget: 480,
    external: [],
    source: `import { mountHashActive } from "../../lib/hash-active";
             globalThis.x = mountHashActive;`,
  },
  {
    name: "icons",
    budget: 3200,
    external: [],
    source: `import { icons } from "../../lib/icons";
             globalThis.x = icons;`,
  },
  {
    name: "decode",
    budget: 1260,
    external: [],
    source: `import { observeDecode } from "../../lib/decode";
             globalThis.x = observeDecode;`,
  },
  {
    name: "reading-progress",
    budget: 740,
    external: [],
    source: `import { mountReadingProgress } from "../../lib/reading-progress";
             globalThis.x = mountReadingProgress;`,
  },
  {
    name: "overlay-stack",
    budget: 1000,
    external: [],
    source: `import { pushOverlay, portal, trapFocus } from "../../lib/overlay-stack";
             globalThis.x = [pushOverlay, portal, trapFocus];`,
  },
];

const CSS_ENTRIES: { name: string; budget: number }[] = [
  { name: "elements/typography", budget: 1320 },
  { name: "elements/layout", budget: 1210 },
  { name: "elements/input", budget: 1230 },
  { name: "elements/tooltip", budget: 730 },
  { name: "elements/table", budget: 700 },
  { name: "tags", budget: 2020 },
  { name: "reveal", budget: 520 },
  { name: "orbs", budget: 1000 },
  { name: "a11y", budget: 560 },
  { name: "elements/nav-link", budget: 1350 },
  { name: "elements/button", budget: 1470 },
  { name: "elements/card", budget: 1460 },
  { name: "elements/icon", budget: 280 },
  { name: "elements/brand-mark", budget: 900 },
  { name: "elements/nav", budget: 2400 },
  { name: "elements/footer", budget: 1100 },
  { name: "elements/shell", budget: 3800 },
  { name: "elements/badge", budget: 320 },
  { name: "elements/chip", budget: 340 },
  { name: "elements/empty-state", budget: 500 },
  { name: "elements/field", budget: 760 },
  { name: "elements/search-input", budget: 520 },
  { name: "elements/skeleton", budget: 700 },
  { name: "elements/stat-tile", budget: 980 },
  { name: "elements/toggle", budget: 820 },
  { name: "elements/alert", budget: 730 },
  { name: "elements/area-series", budget: 390 },
  { name: "elements/aurora", budget: 450 },
  { name: "elements/bg-orbs", budget: 370 },
  { name: "elements/brackets", budget: 660 },
  { name: "elements/deck-list", budget: 350 },
  { name: "elements/editor-footer", budget: 640 },
  { name: "elements/error-scene", budget: 1910 },
  { name: "elements/management-row", budget: 600 },
  { name: "elements/modal", budget: 910 },
  { name: "elements/overview-grid", budget: 420 },
  { name: "elements/page-hero", budget: 1090 },
  { name: "elements/reading-progress", budget: 450 },
  { name: "elements/save-status", budget: 270 },
  { name: "elements/section-heading", budget: 560 },
  { name: "elements/surface", budget: 980 },
  { name: "elements/text-link", budget: 1290 },
  { name: "elements/toast", budget: 960 },
  { name: "elements/progress-bar", budget: 650 },
  { name: "elements/step-list", budget: 850 },
  { name: "elements/log-tail", budget: 440 },
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
