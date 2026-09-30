
import { mkdirSync, rmSync, writeFileSync } from "node:fs";

const DIR = new URL("./.fixtures/", import.meta.url).pathname;

const ENTRIES: {
  name: string;
  budget: number;
  external: string[];
  source: string;
}[] = [
  {
    // Shared filtering/navigation: 364 B gzip measured 2026-09-27,
    // +150 B platform delta and ~10% room.
    name: "select",
    budget: 580,
    external: [],
    source: `import { filterSelectOptions, nextEnabledOption } from "../../lib/select";
             globalThis.x = { filterSelectOptions, nextEnabledOption };`,
  },
  {
    // Locale popover anchoring and keys: 1017 B gzip measured 2026-09-30,
    // +150 B platform delta and ~10% room.
    name: "lang-switch",
    budget: 1290,
    external: [],
    source: `import { mountLangSwitch } from "../../lib/lang-switch";
             globalThis.x = mountLangSwitch;`,
  },
  {
    // Six circle flags (br de es fr gb ru): 1296 B gzip measured 2026-09-30,
    // +150 B platform delta and ~10% room.
    name: "flags",
    budget: 1600,
    external: [],
    source: `import { flagBody } from "../../lib/flags";
             globalThis.x = flagBody;`,
  },
  {
    // Astro's optional-search picker plus shared overlay/focus. Raised from 4540
    // (2026-09-28) for measured below-field placement and hover that follows
    // the pointer through scrolls: 4608 B gzip measured, +150 B platform delta
    // and ~4% room.
    name: "astro-select",
    budget: 4950,
    external: [],
    source: `import { enhanceAstroSelect } from "../../lib/astro-select";
             globalThis.x = enhanceAstroSelect;`,
  },
  {
    name: "light-field",
    budget: 1400,
    external: [],
    source: `import { field } from "../../lib/light-field";
             globalThis.x = field;`,
  },
  {
    // Raised from 1700 (2026-09-30) to keep the cursor above top-layer popovers:
    // 1760 B gzip measured, +150 B platform delta and ~10% room.
    name: "cursor-engine",
    budget: 2100,
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
    // 761 B gzip measured 2026-09-29: now bundles raf-loop and motion-query, room ~10%.
    name: "count-up",
    budget: 840,
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
    // 3762 B gzip measured 2026-09-28 after 11 icons for audit call sites,
    // +150 B platform delta and ~10% room.
    name: "icons",
    budget: 4310,
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
  {
    // 358 B gzip measured 2026-09-28, +150 B platform delta and ~10% room.
    name: "hotkeys",
    budget: 560,
    external: [],
    source: `import * as m from "../../lib/hotkeys";
             globalThis.x = m;`,
  },
  {
    // 899 B gzip measured 2026-09-28, +150 B platform delta and ~10% room.
    name: "parallax",
    budget: 1160,
    external: [],
    source: `import * as m from "../../lib/parallax";
             globalThis.x = m;`,
  },
  {
    // 1114 B gzip measured 2026-09-29: rect cache and the shared frame loop, room ~10%.
    name: "tilt",
    budget: 1230,
    external: [],
    source: `import * as m from "../../lib/tilt";
             globalThis.x = m;`,
  },
  {
    // 532 B gzip measured 2026-09-28, +150 B platform delta and ~10% room.
    name: "live-poll",
    budget: 760,
    external: [],
    source: `import * as m from "../../lib/live-poll";
             globalThis.x = m;`,
  },
  {
    // 1444 B gzip measured 2026-09-28, +150 B platform delta and ~10% room.
    name: "line-series",
    budget: 1760,
    external: [],
    source: `import * as m from "../../lib/line-series";
             globalThis.x = m;`,
  },
  {
    // 763 B gzip measured 2026-09-28, +150 B platform delta and ~10% room.
    name: "roving-focus",
    budget: 1010,
    external: [],
    source: `import * as m from "../../lib/roving-focus";
             globalThis.x = m;`,
  },
  {
    name: "i18n",
    // Lookup plus six bundled catalogs (en/fr/es/pt-br/de/ru): 3636 B gzip
    // measured in Linux CI 2026-09-30; +150 B platform variance and ~10% room.
    budget: 4200, // 564 B room for the four deliberately added language catalogs.
    external: [],
    source: `import { createUiI18n, uiText, resolveUiLocale } from "../../lib/i18n";
             globalThis.x = [createUiI18n, uiText, resolveUiLocale];`,
  },
];

const CSS_ENTRIES: { name: string; budget: number }[] = [
  { name: "elements/typography", budget: 1680 }, // 1370 B: text tones, truncate, heading title/label, code wrap (2026-09-29)
  { name: "elements/layout", budget: 1310 }, // 1035 B: grid stackAt md (2026-09-29)
  { name: "elements/input", budget: 1230 },
  // Custom select styling: 811 B gzip, +150 B platform variance and ~10% room.
  { name: "elements/select", budget: 1060 },
  { name: "elements/tooltip", budget: 730 },
  { name: "elements/table", budget: 830 }, // 596 B: min width hook (2026-09-29)
  { name: "elements/tag", budget: 1000 }, // 760 B: split from tags.css (2026-09-29)
  { name: "elements/mark", budget: 890 }, // 652 B: mark, sweep, drawline, split from tags.css (2026-09-29)
  { name: "elements/tabs", budget: 1440 }, // 1169 B: split from tags.css (2026-09-29)
  { name: "elements/page", budget: 830 }, // 604 B: page head, toolbar, scroller, split from shell.css (2026-09-29)
  { name: "reveal", budget: 520 },
  { name: "orbs", budget: 1000 },
  { name: "a11y", budget: 560 },
  { name: "elements/nav-link", budget: 1350 },
  { name: "elements/button", budget: 1870 }, // 1547 B: brand variant, static span, icon danger (2026-09-29)
  { name: "elements/card", budget: 1460 },
  { name: "elements/icon", budget: 280 },
  { name: "elements/brand-mark", budget: 900 },
  { name: "elements/nav", budget: 3450 }, // 2981 B: flagged locale popover replaced the code row (2026-09-30)
  { name: "elements/footer", budget: 1100 },
  { name: "elements/shell", budget: 3200 }, // 2818 B after the page head split (2026-09-29)
  { name: "elements/badge", budget: 320 },
  { name: "elements/chip", budget: 1110 }, // 864 B: chip base and tier tones, split from tags.css (2026-09-29)
  { name: "elements/empty-state", budget: 500 },
  { name: "elements/field", budget: 760 },
  { name: "elements/search-input", budget: 520 },
  { name: "elements/skeleton", budget: 700 },
  { name: "elements/stat-tile", budget: 980 },
  { name: "elements/toggle", budget: 820 },
  { name: "elements/alert", budget: 1100 }, // 847 B: tip/note callouts, exit action, phone stack and second row (2026-09-29)
  { name: "elements/area-series", budget: 390 },
  { name: "elements/aurora", budget: 450 },
  { name: "elements/bg-orbs", budget: 370 },
  { name: "elements/brackets", budget: 660 },
  { name: "elements/deck-list", budget: 340 }, // 157 B: list reset (2026-09-29)
  { name: "elements/editor-footer", budget: 640 },
  { name: "elements/error-scene", budget: 1910 },
  { name: "elements/management-row", budget: 1040 }, // 788 B: link, static, title/meta, wrap and stacked actions (2026-09-29)
  { name: "elements/modal", budget: 1690 }, // 1380 B: viewer variant and static preview
  { name: "elements/overview-grid", budget: 420 },
  { name: "elements/page-hero", budget: 1090 },
  { name: "elements/reading-progress", budget: 450 },
  { name: "elements/section-heading", budget: 560 },
  { name: "elements/surface", budget: 980 },
  { name: "elements/text-link", budget: 1910 }, // 1583 B: arrow, inline and quiet variants
  { name: "elements/toast", budget: 960 },
  { name: "elements/progress-bar", budget: 1000 }, // 760 B: target marker and segments (2026-09-29)
  { name: "elements/step-list", budget: 2050 }, // 1712 B: navigable checklist form
  { name: "elements/log-tail", budget: 440 },
  // Stats contracts, macOS/arm64: measured gzip bytes +150 B platform variance and ~10% room.
  { name: "elements/ambient-sky", budget: 1130 }, // 869 B
  { name: "elements/counter-card", budget: 1180 }, // 917 B
  { name: "elements/community-card", budget: 1020 }, // 773 B
  { name: "elements/ranking-card", budget: 1190 }, // 928 B
  // Blocks added 2026-09-28, macOS/arm64: measured gzip +150 B platform variance and ~10% room.
  { name: "elements/deck-layout", budget: 400 }, // 206 B
  { name: "elements/fact-list", budget: 930 }, // 687 B
  { name: "elements/pager", budget: 370 }, // 184 B
  { name: "elements/disclosure", budget: 1260 }, // 989 B
  { name: "elements/feed", budget: 740 }, // 516 B
  { name: "elements/radio-group", budget: 1700 }, // 1395 B: fixed columns, rails and card height hook (2026-09-29)
  { name: "elements/slider", budget: 380 }, // 191 B
  { name: "elements/file-drop", budget: 740 }, // 514 B
  { name: "elements/picker-panel", budget: 1360 }, // 1081 B
  { name: "elements/stepper", budget: 1590 }, // 1287 B: compact narrow-screen label (2026-09-29)
  { name: "elements/spinner", budget: 500 }, // 303 B
  { name: "elements/line-series", budget: 940 }, // 704 B
  { name: "elements/skip-link", budget: 480 }, // 283 B
  { name: "elements/popover", budget: 1230 }, // 964 B: bottom placement, touch areas (2026-09-29)
  { name: "elements/copy-surface", budget: 1620 }, // 1319 B
  { name: "elements/profile-menu", budget: 2010 }, // 1675 B: menu height cap, coarse-pointer targets (2026-09-29)
  { name: "elements/nav-progress", budget: 510 }, // 310 B: nav progress bar (2026-09-29)
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
