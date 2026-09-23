/**
 * Bundle size gate for @bagel/kit: the bolota pattern (bolota/scripts/
 * size.ts) applied to the entries the console apps bundle rather than
 * externalise.
 *
 * Measured through synthetic consumers rather than by building a module
 * directly: an entry with no importer tree-shakes to nothing, which reports a
 * flattering number no real app ever sees. Each consumer imports exactly what
 * one real route pulls in, so the number tracks what that page's visitor
 * actually pays for its chunk.
 *
 * Both console apps run this same script (dashboard and admin both call it from
 * their build), so a row here is budgeted once no matter which app is bundling
 * it.
 *
 * Budgets are per entry, gzip bytes, and deliberately carry a few percent of
 * headroom over the measurement: gzip output differs slightly by platform
 * (bolota once failed CI on a ~100 B linux/x64 vs macOS/arm64 delta with no
 * source change). Raising one is allowed and expected when a feature
 * genuinely costs bytes: record what grew, the measured figure, and the
 * room left, in a comment next to the number, in the same commit.
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
    // The dashboard shell receives category links from its server load. Keep
    // MODULE_CATALOG out of this client entry: counting its categories in the
    // browser added 56 KB raw / 17 KB gzip to the production shell (2026-09-14).
    // Measured 678 B gzip on macOS/arm64; 900 allows the platform delta and
    // small registry additions without accommodating a catalog import.
    name: "dashboard nav registry",
    budget: 900,
    external: [],
    source: `import { sectionForPath, dashboardNavItems, dashboardNavGroups } from "../../lib/nav-dashboard";
             globalThis.x = [sectionForPath, dashboardNavItems, dashboardNavGroups];`,
  },
  {
    // The client-side imports of the dashboard's import page: the Moobot JSON
    // parser, the shared per-kind caps, and the source strategy registry.
    // Initial measurement 2026-08-24: 18577 B gzip at introduction of this
    // gate (parser ported from the Go importer service when it folded into the
    // dashboard; the engine-split refactor of PR #648 is inside this number).
    // Largest row here and client-facing: the first candidate for a lazy
    // import if the import page's chunk ever needs to shrink.
    //
    // 2026-09-07, source-strategy split: the page now pulls
    // lib/importer/strategy.ts, which statically imports the same parseMoobot
    // it already bundled, so the entry gained the registry alone: 6849 ->
    // 7496 B gzip on macOS/arm64 (+647 B). Budget untouched, still 11.6 KB of
    // room; the two figures also record how far the row has drifted below the
    // number it was budgeted against.
    name: "import page (moobot + caps)",
    budget: 19100,
    external: [],
    source: `import { parseMoobot } from "../../lib/importer/moobot";
             import { applyImportCaps } from "../../lib/importer/caps";
             import { IMPORT_STRATEGIES } from "../../lib/importer/strategy";
             globalThis.x = [parseMoobot, applyImportCaps, IMPORT_STRATEGIES];`,
  },
  {
    // The admin console's shell: the section registry plus the three resolvers
    // its (admin)/+layout.svelte calls on every navigation. Budgeted because
    // this row is really a guard on WHICH nav module the shell imports.
    // Initial measurement 2026-09-09 (macOS/arm64): 814 B gzip. Budget 1000,
    // not the 895 a flat +10% would give: CI's linux/x64 runner gzips the same
    // bytes ~100-150 B larger than macOS/arm64 (see the size-budgets skill),
    // which on a row this small is 18%, not a rounding error. 814 + 150 = 964
    // -> +3% of the usual headroom.
    //
    // The same three functions imported from lib/nav.ts instead measure 16253 B
    // gzip: nav.ts statically imports MODULE_CATALOG for the dashboard's
    // Modules fan-out, and that is a value, not a type. lib/nav-admin.ts and
    // lib/nav-core.ts exist to keep the dashboard's catalog out of the admin
    // bundle, and this row is what fails the build the day someone "tidies"
    // them back together.
    name: "admin nav registry",
    budget: 1000,
    external: [],
    source: `import { ADMIN_SECTIONS, adminNavGroups, adminSectionForPath, adminSectionLabelKey } from "../../lib/nav-admin";
             globalThis.x = [ADMIN_SECTIONS, adminNavGroups, adminSectionForPath, adminSectionLabelKey];`,
  },
  {
    // Server-side StreamElements parser. Not currently in any client bundle;
    // budgeted so the day it is pulled client-side the cost is a deliberate
    // decision, not a silent one. Initial measurement 2026-08-24: 7499 B gzip.
    //
    // Raised from 7750 (2026-08-24) for the urlfetch importer feature: parsing
    // now walks command replies for $(urlfetch ...) tokens and synthesizes
    // deduped ManifestFetch entries (the fetchDefs Map plus the IMPORT_ITEM_CAPS
    // import pulled in for it), landing at 8007 B gzip on macOS/arm64, already
    // over the old budget on its own. On top of that this repo's CI runner is
    // linux/x64, which gzips the same bytes ~100-150 B larger than macOS/arm64
    // (see the size-budgets skill); 8420 covers that delta and keeps the usual
    // ~3% of room above the linux-side estimate (8007 + 150 = 8157 -> +3%).
    //
    // Raised from 8420 (2026-09-09) for the lexer adoption: every {…} span the
    // parser MINTS now goes out through tmpl.ts's intactSpan, which pulls the
    // shared lexer (lex + parseSpan) into this entry. Measured 8473 B gzip on
    // macOS/arm64, 53 B over the old budget. The alternative was a local
    // "does this string contain | or }" check, which is the exact hand-rolled
    // grammar knowledge this change exists to delete — it would have cost ~0 B
    // and gone stale the next time the span grammar spends a byte. 8700 covers
    // the measured figure plus the linux/x64 gzip delta (~150 B) and ~1% room
    // (8473 + 150 = 8623 -> +1%).
    //
    // Raised again from 8700 (2026-09-22, phase 6 importer targets): the
    // positional/slice/fallback family ($(N)/$(N:)/$(:M)/$(N:M)/$(N|fb)),
    // $(pointsname)/$(user.points)/$(time <place>)/$(math)/$(repeat) and the
    // {user}/{channel} per-suffix dispatch functions that replaced
    // bakedIdentity's single-target table rows. Measured 9080 B gzip on
    // macOS/arm64. 9500 covers that plus the linux/x64 delta (~150 B) and
    // ~3% room (9080 + 150 = 9230 -> +3%).
    //
    // Re-measured 2026-09-22 (review round, no budget change): the E-inverse
    // round-trip fixes (rangeKey/pickKey/chooseParam/the $(:M) trigger-word
    // shift/the $(if …) warn wording) moved this to 9287 B gzip on
    // macOS/arm64 — still inside the 9500 budget above (213 B room), so the
    // number is recorded here rather than raised.
    name: "streamelements parser",
    budget: 9500,
    external: [],
    source: `import { parseStreamElements } from "../../lib/importer/streamelements";
             globalThis.x = parseStreamElements;`,
  },
  {
    // SERVER-SIDE ONLY (see the module's own header): sql.js is a lazy
    // dynamic import precisely so its ~1.5MB wasm never enters a bundle.
    // sql.js stays external here for the same reason: this row proves the
    // module ITSELF stays light, and blows up loudly if the wasm ever becomes
    // a static import someone tries to bundle. Initial measurement
    // 2026-08-24: 8058 B gzip.
    //
    // Raised from 8300 (2026-08-24): the parser itself hasn't grown, still
    // 8058 B gzip on macOS/arm64, but that left only 242 B (2.9%) of room:
    // too thin to survive CI's linux/x64 runner, which gzips the same bytes
    // ~100-150 B larger than macOS/arm64 (see the size-budgets skill). 8460
    // keeps the usual ~3% of room above the linux-side estimate (8058 + 150 =
    // 8208 -> +3%).
    //
    // Raised again from 8460 (2026-09-22, phase 6 importer targets): the
    // $argN/$numN/$arglN per-slot positional family (replacing the old
    // all-or-nothing rule), $countdown/$countup date normalization
    // (targets.ts's normalizeInstant), and $points/$currencyname/
    // $randusername/$userid's bug fix. Measured 8819 B gzip on macOS/arm64.
    // 9250 covers that plus the linux/x64 delta (~150 B) and ~3% room
    // (8819 + 150 = 8969 -> +3%).
    //
    // Raised again from 9250 (2026-09-22, same day: the three deferred
    // slices): $readapi(URL) now synthesizes a real {urlfetch:…} definition,
    // which pulls nightbot/fetchdefs.ts's slot allocator (shared with every
    // other $(…)-syntax source) into this parser for the first time. Measured
    // 9544 B gzip on macOS/arm64. 10000 covers that plus the linux/x64 delta
    // (~150 B) and ~3% room (9544 + 150 = 9694 -> +3%).
    //
    // Raised again from 10000 (2026-09-22, review round): the fetch-def-
    // created warn (nightbot/fetchdefs.ts's createdFetchDefMessage) and the
    // $numN numeric-check-lost warn both added diagnostics text this parser
    // now carries. Measured 10276 B gzip on macOS/arm64. 10750 covers that
    // plus the linux/x64 delta (~150 B) and ~3% room (10276 + 150 = 10426
    // -> +3%).
    name: "streamlabs desktop parser",
    budget: 10750,
    external: ["sql.js", "node:module"],
    source: `import { parseStreamLabsDesktop } from "../../lib/importer/streamlabs-desktop";
             globalThis.x = parseStreamLabsDesktop;`,
  },
  {
    // The marketing variables guide's "Try it" row
    // (docs/specs/variables-catalog.md D7, second half) pulls rehearsal.ts
    // into the guide's bundle: the lexer, every sample scope and the
    // response-line splitter, none of which the guide otherwise ships.
    // Initial measurement 2026-09-19 (macOS/arm64): 3664 B gzip for
    // rehearseCommand alone; the whole guide module measured 3866 B the day
    // before. Budget 4150: +150 B for CI's linux/x64 runner, which gzips the
    // same bytes ~100-150 B larger than macOS/arm64 (see the size-budgets
    // skill), plus room for the ~300 B of DOM wiring around it.
    name: "guide try-it evaluator (engine/rehearsal)",
    budget: 4150,
    external: [],
    // The guide's evaluate.ts is ~300 B of DOM wiring around rehearseCommand;
    // rehearsal is the whole cost. Measured from the kit side because this
    // script also runs inside the console image builds, where marketing/ is
    // not copied (Containerfile COPY list) and a marketing import fails to
    // resolve. The marketing module itself is exercised by the Astro build.
    source: `import { rehearseCommand } from "../../lib/engine/rehearsal";
             globalThis.x = rehearseCommand;`,
  },
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

rmSync(DIR, { recursive: true, force: true });
if (failed) {
  console.error(
    "\nsize gate failed. If the growth is deliberate, raise the budget in web/kit/scripts/size.ts with what grew, the measured figure, and the room left, in the same commit.",
  );
  process.exit(1);
}
