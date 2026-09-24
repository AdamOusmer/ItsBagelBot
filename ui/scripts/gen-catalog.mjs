// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('..', import.meta.url).pathname;
const CATALOG = join(ROOT, 'CATALOG.md');

const FAMILY = {
  Heading: 'Typography', Text: 'Typography', Eyebrow: 'Typography', Lead: 'Typography',
  Kbd: 'Typography', Code: 'Typography', Label: 'Typography', TextLink: 'Typography',
  VisuallyHidden: 'Typography', SectionHeading: 'Typography',
  Container: 'Layout', Section: 'Layout', Stack: 'Layout', Cluster: 'Layout', Grid: 'Layout',
  Divider: 'Layout', Spacer: 'Layout', AppShell: 'Layout', Scroller: 'Layout',
  InspectorSurface: 'Layout', PageHero: 'Layout', PickerPanel: 'Layout',
  Button: 'Controls', ButtonLink: 'Controls', IconButton: 'Controls', Switch: 'Controls',
  Toggle: 'Controls', Field: 'Controls', FieldError: 'Controls', Input: 'Controls',
  Select: 'Controls', Textarea: 'Controls', Checkbox: 'Controls', RadioGroup: 'Controls',
  SegmentedControl: 'Controls', SearchInput: 'Controls',
  Badge: 'Feedback', NotificationBell: 'Feedback', StatusDot: 'Feedback', Chip: 'Feedback', Tag: 'Feedback', ToastHost: 'Feedback',
  AlertBanner: 'Feedback', Skeleton: 'Feedback', SkeletonStack: 'Feedback',
  EmptyState: 'Feedback', Modal: 'Feedback', ConfirmDialog: 'Feedback', Tooltip: 'Feedback',
  SaveStatus: 'Feedback', ErrorScene: 'Feedback', ProgressBar: 'Feedback', StepList: 'Feedback',
  Nav: 'Navigation', MobileMenu: 'Navigation', NavLink: 'Navigation', NavGroup: 'Navigation',
  Rail: 'Navigation', RailItem: 'Navigation', Topbar: 'Navigation', Dock: 'Navigation',
  SectionNav: 'Navigation', Footer: 'Navigation', Brand: 'Navigation',
  LanguageSwitcher: 'Navigation', SocialRail: 'Navigation', PageHead: 'Navigation',
  PageToolbar: 'Navigation', Hamburger: 'Navigation', EditorFooter: 'Navigation',
  Card: 'Data', CardHead: 'Data', DeckList: 'Data', ManagementRow: 'Data',
  OverviewGrid: 'Data', StatTile: 'Data', AreaSeries: 'Data', Table: 'Data', Icon: 'Data',
  LogTail: 'Data',
  LightField: 'Motion', BackgroundOrbs: 'Motion', AuroraBg: 'Motion', Cursor: 'Motion',
  ReadingProgress: 'Motion', CardAtmosphere: 'Motion', Brackets: 'Motion', Sky: 'Motion',
};

const SINGLE_ADAPTER_REASON = {
  NotificationBell: 'Svelte only: interactive notification popover with caller-owned callbacks.',
  PickerPanel: 'Svelte only: interactive anchored dropdown that becomes a modal sheet on mobile.',
  ToastHost: 'Svelte only: it subscribes to the toast store, and a host with nothing to subscribe to renders nothing.',
  ConfirmDialog: 'Svelte only: a composition of Modal + Button with no CSS of its own, and its two callbacks are the element.',
  Toggle: 'Svelte only: bindable checkbox state; the static spelling is Switch.',
  FieldError: 'Svelte only: it renders only when a form action has returned an error, which a static page has not.',
  ProgressBar: 'Svelte only: its value arrives from a live stream (the deploy run\'s snapshots) and eases between them; a static page has no progress to report.',
  StepList: 'Svelte only: rows change state from a live stream, and the per-row detail is a Snippet that takes the step, which an Astro slot cannot.',
  Sky: 'Svelte only: it follows the pointer and a flow\'s progress from client state; a static page has neither.',
  LogTail: 'Svelte only: it pins itself to the newest line as lines arrive and lets go when the reader scrolls up, which needs a client.',
};

const PROP_RE = /^\s*(?:\/\*\*.*\*\/\s*)?([A-Za-z_$][\w$]*)(\??)\s*:\s*(.+);\s*$/;

function propsFromSvelte(source, file) {
  const start = source.indexOf('}: {');
  if (start === -1) return null;
  const end = source.indexOf('= $props()', start);
  if (end === -1) throw new Error(`${file}: found "}: {" with no "$props()" call after it`);
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

const isNoise = (line) =>
  !line || line.startsWith('//') || line.startsWith('*') || line.startsWith('/*');

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

const tidy = (type) =>
  type.replace(/\s+/g, ' ').trim().replace(/\\/g, '\\\\').replace(/\|/g, '\\|');

function contractOf(source) {
  return [...source.matchAll(/import\s+'\.\.\/(styles\/[^']+\.css)'/g)].map((m) => m[1]);
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
