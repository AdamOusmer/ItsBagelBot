
import { mkdirSync, rmSync, writeFileSync } from "node:fs";

const DIR = new URL("./.fixtures/", import.meta.url).pathname;

const ENTRIES: {
  name: string;
  budget: number;
  external: string[];
  source: string;
}[] = [
  {
    name: "dashboard nav registry",
    budget: 900,
    external: [],
    source: `import { sectionForPath, dashboardNavItems, dashboardNavGroups } from "../../lib/nav-dashboard";
             globalThis.x = [sectionForPath, dashboardNavItems, dashboardNavGroups];`,
  },
  {
    name: "import page (moobot + caps)",
    budget: 19100,
    external: [],
    source: `import { parseMoobot } from "../../lib/importer/moobot";
             import { applyImportCaps } from "../../lib/importer/caps";
             import { IMPORT_STRATEGIES } from "../../lib/importer/strategy";
             globalThis.x = [parseMoobot, applyImportCaps, IMPORT_STRATEGIES];`,
  },
  {
    name: "admin nav registry",
    budget: 1000,
    external: [],
    source: `import { ADMIN_SECTIONS, adminNavGroups, adminSectionForPath, adminSectionLabelKey } from "../../lib/nav-admin";
             globalThis.x = [ADMIN_SECTIONS, adminNavGroups, adminSectionForPath, adminSectionLabelKey];`,
  },
  {
    name: "streamelements parser",
    budget: 9500,
    external: [],
    source: `import { parseStreamElements } from "../../lib/importer/streamelements";
             globalThis.x = parseStreamElements;`,
  },
  {
    name: "streamlabs desktop parser",
    budget: 10750,
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
    "\nsize gate failed. If the growth is deliberate, raise the budget in web/kit/scripts/size.ts with what grew, the measured figure, and the room left, in the same commit.",
  );
  process.exit(1);
}
