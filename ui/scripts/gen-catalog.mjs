// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Generates ui/CATALOG.md — the readable index of every block this library
 * ships: its family, its props, which adapters exist, and the contract file
 * whose class names it emits.
 *
 * WHY IT IS GENERATED AND COMMITTED, rather than either hand-written or built
 * on demand. Hand-written, it is wrong within a week and nobody notices,
 * because nothing reads it in CI. Built on demand, it is not in the diff, so a
 * reviewer looking at a PR that adds an element cannot see that the element
 * arrived with one adapter and no props documented. Generated AND committed
 * with a `--check` mode wired into `bun run check` gets both: the file is in
 * the diff, and a stale file fails the gate.
 *
 * The props come from the adapters' own type declarations — the `$props()`
 * type literal on the Svelte side and `interface Props` on the Astro side —
 * so the catalog cannot drift from the signature. Deliberately a text parse
 * and not a TypeScript program: this runs on every `check`, the shapes are a
 * fixed set of five (see PROP_RE below), and a compiler pass over 120 files to
 * read a property list costs seconds per run for a stricter answer than the
 * question needs. When a declaration does not parse, the script FAILS rather
 * than emitting an empty prop list — a silently empty row is the failure mode
 * that would make the whole file untrustworthy.
 *
 * FAMILY IS DECLARED HERE, NOT INFERRED. There is no signal in a file that
 * says whether it is navigation or feedback, and a heuristic on the name would
 * put `SectionNav` and `SectionHeading` in the same bucket. The map below is
 * exhaustive and the script fails on an unclassified adapter, which is what
 * makes adding a block a decision about where it belongs instead of an
 * accident.
 */

import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('..', import.meta.url).pathname;
const CATALOG = join(ROOT, 'CATALOG.md');

/** Block -> family. Exhaustive; an adapter missing from here fails the run. */
const FAMILY = {
  // Typography
  Heading: 'Typography', Text: 'Typography', Eyebrow: 'Typography', Lead: 'Typography',
  Kbd: 'Typography', Code: 'Typography', Label: 'Typography', TextLink: 'Typography',
  VisuallyHidden: 'Typography', SectionHeading: 'Typography',
  // Layout
  Container: 'Layout', Section: 'Layout', Stack: 'Layout', Cluster: 'Layout', Grid: 'Layout',
  Divider: 'Layout', Spacer: 'Layout', AppShell: 'Layout', Scroller: 'Layout',
  InspectorSurface: 'Layout', PageHero: 'Layout',
  // Controls
  Button: 'Controls', ButtonLink: 'Controls', IconButton: 'Controls', Switch: 'Controls',
  Toggle: 'Controls', Field: 'Controls', FieldError: 'Controls', Input: 'Controls',
  Select: 'Controls', Textarea: 'Controls', Checkbox: 'Controls', RadioGroup: 'Controls',
  SegmentedControl: 'Controls', SearchInput: 'Controls',
  // Feedback
  Badge: 'Feedback', Chip: 'Feedback', Tag: 'Feedback', ToastHost: 'Feedback',
  AlertBanner: 'Feedback', Skeleton: 'Feedback', SkeletonStack: 'Feedback',
  EmptyState: 'Feedback', Modal: 'Feedback', ConfirmDialog: 'Feedback', Tooltip: 'Feedback',
  SaveStatus: 'Feedback', ErrorScene: 'Feedback',
  // Navigation
  Nav: 'Navigation', MobileMenu: 'Navigation', NavLink: 'Navigation', NavGroup: 'Navigation',
  Rail: 'Navigation', RailItem: 'Navigation', Topbar: 'Navigation', Dock: 'Navigation',
  SectionNav: 'Navigation', Footer: 'Navigation', Brand: 'Navigation',
  LanguageSwitcher: 'Navigation', SocialRail: 'Navigation', PageHead: 'Navigation',
  PageToolbar: 'Navigation', Hamburger: 'Navigation', EditorFooter: 'Navigation',
  // Data
  Card: 'Data', CardHead: 'Data', DeckList: 'Data', ManagementRow: 'Data',
  OverviewGrid: 'Data', StatTile: 'Data', AreaSeries: 'Data', Table: 'Data', Icon: 'Data',
  // Motion & background
  LightField: 'Motion', BackgroundOrbs: 'Motion', AuroraBg: 'Motion', Cursor: 'Motion',
  ReadingProgress: 'Motion', CardAtmosphere: 'Motion', Brackets: 'Motion',
};

/**
 * Blocks that ship ONE adapter on purpose, with the reason. Anything else
 * missing a twin is a gap, and the catalog says so in the Adapters column;
 * these say why instead, so "svelte only" never becomes background noise that
 * hides a real omission.
 */
const SINGLE_ADAPTER_REASON = {
  ToastHost: 'Svelte only: it subscribes to the toast store, and a host with nothing to subscribe to renders nothing.',
  ConfirmDialog: 'Svelte only: a composition of Modal + Button with no CSS of its own, and its two callbacks are the element.',
  Toggle: 'Svelte only: bindable checkbox state; the static spelling is Switch.',
  FieldError: 'Svelte only: it renders only when a form action has returned an error, which a static page has not.',
};

// One line, one prop: `name?: type;`. The type runs to the line's final
// semicolon rather than to the first one, because an inline object type
// (`readonly { value: string; label: string }[]`) contains semicolons of its
// own and a non-greedy match drops the prop entirely — which is worse than a
// slightly long cell, since a missing REQUIRED prop is what a reader would
// most rely on this file for.
const PROP_RE = /^\s*(?:\/\*\*.*\*\/\s*)?([A-Za-z_$][\w$]*)(\??)\s*:\s*(.+);\s*$/;

function propsFromSvelte(source, file) {
  const start = source.indexOf('}: {');
  if (start === -1) return null;
  const end = source.indexOf('= $props()', start);
  if (end === -1) throw new Error(`${file}: found "}: {" with no "$props()" call after it`);
  // Cut at the brace that closes the props literal rather than at the
  // `= $props()` itself: Scroller and anything else that spreads a framework
  // attribute type writes `} & HTMLAttributes<HTMLDivElement> = $props()`, and
  // slicing to the call would leave that intersection inside the body.
  const literal = source.slice(start + 4, end);
  return parseProps(literal.slice(0, literal.lastIndexOf('}')));
}

function propsFromAstro(source, file) {
  const start = source.indexOf('interface Props {');
  if (start === -1) return null;
  const end = source.indexOf('\n}', start);
  if (end === -1) throw new Error(`${file}: "interface Props {" is never closed at column 0`);
  return parseProps(source.slice(start + 'interface Props {'.length, end));
}

/**
 * Pull `name?: type` pairs out of a declaration body.
 *
 * Nested object and function types are skipped rather than flattened: a prop
 * typed `{ a: string; b: number }` would otherwise contribute `a` and `b` as
 * if they were props of the element. Depth is tracked on braces and parens,
 * and only depth-0 lines are read.
 */
/** Lines that carry no prop: blank, or part of a JSDoc/line comment. */
const isNoise = (line) =>
  !line || line.startsWith('//') || line.startsWith('*') || line.startsWith('/*');

/** `class` and `children` are on nearly every element and say nothing about
 *  it; listing them 81 times would bury the props that differ. */
const isInteresting = (prop) => {
  const bare = prop.name.replace(/\*$/, '');
  return bare !== 'class' && bare !== 'children';
};

function collectProp(props, rawLine) {
  const match = PROP_RE.exec(rawLine);
  if (!match || match[1] === '[key') return;
  props.push({ name: match[1] + (match[2] === '?' ? '' : '*'), type: tidy(match[3]) });
}

function parseProps(body) {
  const props = [];
  let depth = 0;
  for (const rawLine of body.split('\n')) {
    if (depth === 0 && !isNoise(rawLine.trim())) collectProp(props, rawLine);
    depth = Math.max(0, depth + countDepth(rawLine));
  }
  return props.filter(isInteresting);
}

const countDepth = (line) =>
  (line.match(/[{(]/g) ?? []).length - (line.match(/[})]/g) ?? []).length;

const tidy = (type) => type.replace(/\s+/g, ' ').trim().replace(/\|/g, '\\|');

/** The contract stylesheet an adapter imports, if it imports one. */
function contractOf(source) {
  return [...source.matchAll(/import\s+'(\.\.\/styles\/[^']+\.css)'/g)].map((m) =>
    m[1].replace('../', ''),
  );
}

function read(dir, ext) {
  const out = new Map();
  for (const file of readdirSync(join(ROOT, dir)).sort()) {
    if (!file.endsWith(ext)) continue;
    const name = file.slice(0, -ext.length);
    if (name === 'index') continue;
    out.set(name, readFileSync(join(ROOT, dir, file), 'utf8'));
  }
  return out;
}

const svelte = read('svelte', '.svelte');
const astro = read('astro', '.astro');
const names = [...new Set([...svelte.keys(), ...astro.keys()])].sort();

const unclassified = names.filter((n) => !FAMILY[n]);
if (unclassified.length) {
  console.error(
    `gen-catalog: unclassified adapter(s): ${unclassified.join(', ')}\n` +
      'Add each to the FAMILY map in scripts/gen-catalog.mjs. Family is declared,\n' +
      'not inferred, so that adding a block is a decision about where it belongs.',
  );
  process.exit(1);
}

const FAMILIES = ['Typography', 'Layout', 'Controls', 'Feedback', 'Navigation', 'Data', 'Motion'];

const rows = names.map((name) => {
  const sv = svelte.get(name);
  const as = astro.get(name);
  const props = (sv ? propsFromSvelte(sv, `svelte/${name}.svelte`) : null) ??
    (as ? propsFromAstro(as, `astro/${name}.astro`) : null) ?? [];
  const adapters = [sv && 'svelte', as && 'astro'].filter(Boolean).join(' + ');
  return {
    name,
    family: FAMILY[name],
    props,
    adapters,
    // Union of both adapters' imports: several elements were written with the
    // stylesheet imported on one side only, and the catalog's job is to name
    // the contract, not to reproduce that asymmetry.
    contract: [...new Set([...contractOf(sv ?? ''), ...contractOf(as ?? '')])].sort().join(', '),
    note: !sv || !as ? (SINGLE_ADAPTER_REASON[name] ?? '') : '',
  };
});

let body = `<!-- GENERATED by scripts/gen-catalog.mjs. Do not edit by hand.
     Run \`bun scripts/gen-catalog.mjs\` after adding or changing an adapter;
     \`bun run check\` fails when this file is stale. -->

# @bagel/ui — block catalog

Every block the library ships, by family. **${rows.length}** blocks;
**${rows.filter((r) => r.adapters.includes('+')).length}** ship both adapters.

A \`*\` after a prop name means it is required. \`class\` and \`children\` are
omitted: nearly every block takes both, and listing them ${rows.length} times
would bury the props that differ. Every block also forwards unknown attributes
to its outermost element.

Import from \`@bagel/ui/svelte\` or \`@bagel/ui/astro\` (barrels), or from the
per-file subpath when you want exactly one element's CSS in the bundle.
`;

for (const family of FAMILIES) {
  const inFamily = rows.filter((r) => r.family === family);
  if (!inFamily.length) continue;
  body += `\n## ${family}\n\n| Block | Props | Adapters | Contract |\n| --- | --- | --- | --- |\n`;
  for (const row of inFamily) {
    const props = row.props.length
      ? row.props.map((p) => `\`${p.name}\`: ${p.type}`).join('<br>')
      : '—';
    const adapters = row.note ? `${row.adapters}<br>*${row.note}*` : row.adapters;
    body += `| **${row.name}** | ${props} | ${adapters} | ${row.contract ? `\`${row.contract}\`` : '—'} |\n`;
  }
}

/* The barrels have to export every block, and the only reliable way to know
   that is to compare them with the same directory listing this file is built
   from. Checked here rather than in a fourth script because the enumeration is
   already done and because a block missing from a barrel and a block missing
   from the catalog are the same mistake. */
for (const [dir, ext, source] of [
  ['svelte', '.svelte', svelte],
  ['astro', '.astro', astro],
]) {
  const barrel = readFileSync(join(ROOT, dir, 'index.ts'), 'utf8');
  const missing = [...source.keys()].filter(
    (name) => !barrel.includes(`from './${name}${ext}'`),
  );
  if (missing.length) {
    console.error(
      `gen-catalog: ${dir}/index.ts does not export: ${missing.join(', ')}\n` +
        'Every adapter is part of the public surface; add it to the barrel.',
    );
    process.exit(1);
  }
}

const check = process.argv.includes('--check');
const current = (() => {
  try {
    return readFileSync(CATALOG, 'utf8');
  } catch {
    return null;
  }
})();

if (check) {
  if (current !== body) {
    console.error(
      'gen-catalog: CATALOG.md is stale. Run `bun scripts/gen-catalog.mjs` and commit the diff.',
    );
    process.exit(1);
  }
  console.log(`catalog is current (${rows.length} blocks)`);
} else {
  writeFileSync(CATALOG, body);
  console.log(`wrote CATALOG.md (${rows.length} blocks)`);
}
