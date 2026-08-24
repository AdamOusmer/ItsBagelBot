/**
 * Bundle size gate for @bagel/shared — the bolota pattern (bolota/scripts/
 * size.ts) applied to the entries the console apps bundle rather than
 * externalise.
 *
 * Measured through synthetic consumers rather than by building a module
 * directly: an entry with no importer tree-shakes to nothing, which reports a
 * flattering number no real app ever sees. Each consumer imports exactly what
 * the dashboard's import page (routes/(app)/settings/import) pulls in, so the
 * number tracks what a broadcaster's browser actually pays for that route
 * chunk.
 *
 * Budgets are per entry, gzip bytes, and deliberately carry a few percent of
 * headroom over the measurement: gzip output differs slightly by platform
 * (bolota once failed CI on a ~100 B linux/x64 vs macOS/arm64 delta with no
 * source change). Raising one is allowed and expected when a feature
 * genuinely costs bytes — record what grew, the measured figure, and the
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
    // The two client-side imports of the dashboard's import page: the Moobot
    // JSON parser plus the shared per-kind caps. Initial measurement
    // 2026-08-24: 18577 B gzip at introduction of this gate (parser ported
    // from the Go importer service when it folded into the dashboard; the
    // engine-split refactor of PR #648 is inside this number). Largest row
    // here and client-facing — the first candidate for a lazy import if the
    // import page's chunk ever needs to shrink.
    name: "import page (moobot + caps)",
    budget: 19100,
    external: [],
    source: `import { parseMoobot } from "../../lib/importer/moobot";
             import { applyImportCaps } from "../../lib/importer/caps";
             globalThis.x = [parseMoobot, applyImportCaps];`,
  },
  {
    // Server-side StreamElements parser. Not currently in any client bundle;
    // budgeted so the day it is pulled client-side the cost is a deliberate
    // decision, not a silent one. Initial measurement 2026-08-24: 7499 B gzip.
    name: "streamelements parser",
    budget: 7750,
    external: [],
    source: `import { parseStreamElements } from "../../lib/importer/streamelements";
             globalThis.x = parseStreamElements;`,
  },
  {
    // SERVER-SIDE ONLY (see the module's own header): sql.js is a lazy
    // dynamic import precisely so its ~1.5MB wasm never enters a bundle.
    // sql.js stays external here for the same reason — this row proves the
    // module ITSELF stays light, and blows up loudly if the wasm ever becomes
    // a static import someone tries to bundle. Initial measurement
    // 2026-08-24: 8058 B gzip.
    name: "streamlabs desktop parser",
    budget: 8300,
    external: ["sql.js", "node:module"],
    source: `import { parseStreamLabsDesktop } from "../../lib/importer/streamlabs-desktop";
             globalThis.x = parseStreamLabsDesktop;`,
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
    "\nsize gate failed. If the growth is deliberate, raise the budget in console/shared/scripts/size.ts with what grew, the measured figure, and the room left — in the same commit.",
  );
  process.exit(1);
}
