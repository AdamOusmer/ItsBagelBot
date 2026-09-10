#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Block guard for the four surfaces, shaped like ./assert-ui-only-in-ui.mjs
// and ./assert-demo-gated.mjs: a source-level scan that names the file and the
// line rather than a rule nobody runs.
//
// THE RULE. A component `<style>` block in marketing/, dashboard/, admin/ or
// docs/ may not contain a rule whose selector targets a BARE HTML ELEMENT
// (h1-h6, p, span, small, code, a, button, input, select, textarea, table) or
// a `.bb-*` contract class.
//
// WHY THOSE TWO SHAPES AND NOTHING ELSE.
//
//   A bare element selector is a component saying "headings look like this
//   HERE". It is invisible from the design library, it wins locally, and it is
//   how the type scale ended up existing in 75 places at once — measured on
//   this tree on 2026-09-10, three quarters of them restating a value the
//   shared scale already had, with no way to tell which quarter was
//   deliberate. The replacement is a block: `<Heading level={2}>`,
//   `<Text size="sm" tone="muted">`, `<Button>`, `<Field>`.
//
//   A `.bb-*` selector is a component reaching INTO a contract it does not
//   own. Sometimes it is a redeclaration (`.bb-chip:disabled { opacity: .5 }`
//   forking the contract's own .55), sometimes a reach-in through :global.
//   Both are the failure the library exists to delete, and both are silent:
//   the contract still says one thing and the page still shows another. The
//   replacement is a modifier on the contract, or a custom property the
//   contract already exposes.
//
// WHAT IT DELIBERATELY DOES NOT FLAG. Page-specific COMPOSITION — where a
// component's own children sit in a grid, how much air a particular page band
// takes, a one-off `position: absolute` for an ornament. Those are selectors
// on the component's own class names, they are genuinely local, and a library
// that tried to own them would have a prop for every page. The gate only fires
// on the two shapes above.
//
// MARKDOWN CONTENT IS EXEMPT and is not scanned at all: .md/.mdx bodies are
// authored prose, the author does not write class names, and the bare-element
// fallback in @bagel/ui/styles/elements/typography.css exists precisely to
// dress them. A component that styles a markdown body it renders is the one
// case where a bare selector is right, and it takes an allowlist row saying so.
//
// WHAT COUNTS AS AN OFFENCE, precisely:
//   * ANY rule whose selector names a `.bb-*` class.
//   * A rule on a bare element that sets TYPE (font, font-size, font-family,
//     font-weight, line-height, letter-spacing, text-transform, color).
// A bare-element rule that only COMPOSES — margin, display, grid placement,
// position — is page-specific layout and is deliberately not flagged. The
// library has no opinion about where a page's own children sit.
//
// TWO LISTS, AND THEY MEAN DIFFERENT THINGS.
//
//   ALLOWLIST is permanent and justified. A file here is not debt: the rule it
//   keeps is correct and a block would be wrong. Four entries, each with the
//   argument written out. Adding one is a review conversation, not an edit.
//
//   PENDING is a RATCHET over the debt this gate was written to measure. Each
//   entry is a file and the number of offending rules it had when the gate
//   landed; the file may have that many or fewer, never more. A new offence in
//   a pending file fails. A file not in either list may have none at all.
//
// The ratchet exists because the honest alternative was worse. On the day this
// landed the four surfaces held 264 such rules in 68 files, and converting all
// of them in one change would have meant rewriting markup across 68 files with
// no way to see the result — a visual regression is not something a test
// suite here can catch. Writing a fabricated "justified" reason for each of
// them would have been worse still: an allowlist whose reasons are boilerplate
// stops being read, and then the four real reasons above stop being read with
// it. A number that can only go down says what is true: this is debt, it is
// counted, and it cannot grow.
//
// To pay one down: convert the file's rules to blocks, then lower or delete
// its PENDING number in the same commit. The gate prints the total on every
// run, so the number is in the CI log of every PR.

import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

const webRoot = resolve(import.meta.dir, '../..');
const SURFACES = ['marketing/src', 'dashboard/src', 'admin/src', 'docs/src'];

/** Bare element selectors a component may not restyle. */
const ELEMENTS = [
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'p', 'span', 'small', 'code', 'a', 'button', 'input', 'select', 'textarea', 'table',
];

/**
 * Files allowed to keep such a rule, with the reason and the selectors it
 * covers. Path is relative to web/.
 */
const ALLOWLIST = new Map([
  [
    'marketing/src/components/home/Header.astro',
    'The hero wordmark. Its `h1` rules are a per-glyph motion rig (three ' +
      'breakpoint clamps, a .line/.glyph split the entrance animation drives), ' +
      'not a type size — the element is the animation. Moving it into the ' +
      'library would ship a landing-page motion to every surface.',
  ],
  [
    'dashboard/src/routes/(public)/login/+page.svelte',
    'The console half of the same hero wordmark, deliberately the same rig so ' +
      'the two front doors match. Same reason: motion, not type.',
  ],
  [
    'marketing/src/components/guides/widgets/PathPicker.astro',
    'A widget that BUILDS its <button> rows in a script and styles them by ' +
      'element because they have no stable class at author time. The rows are ' +
      'a JSON tree view, not controls the button contract covers.',
  ],
  [
    'docs/src/components/MermaidStyles.astro',
    'Dresses the SVG mermaid renders at runtime. Every selector here reaches ' +
      'into a DOM this repo does not author and cannot add class names to — ' +
      'mermaid names its own nodes — so `pre.mermaid svg … text|span|p` is ' +
      'the only handle there is.',
  ],
  [
    'marketing/src/components/guides/GuideShell.astro',
    'Dresses an authored markdown body (`:global(p:not([class]))` and its ' +
      'siblings). The one case a bare selector is correct: the guide author ' +
      'writes prose, not class names.',
  ],
  [
    'marketing/src/components/legal/LegalShell.astro',
    'Same as GuideShell: the terms and privacy bodies are authored markdown.',
  ],
  [
    'marketing/src/components/builder/CommandBuilder.astro',
    'Builds its palette, variable and recipe rows in a script (document ' +
      'createElement, no class at author time) and styles them by element for ' +
      'the same reason PathPicker does.',
  ],
]);

/**
 * The ratchet. File -> the number of offending rules it held when this gate
 * landed (2026-09-10). The number may go DOWN and never up; the gate fails
 * both on a file that grows past its number and on a file that shrank without
 * its number being lowered in the same commit, so paying debt down is
 * recorded rather than quietly banked.
 *
 * These are NOT justified. Each one is a rule that should become a block, and
 * the reason it is a number instead of a fix is in the header: 264 rules in 68
 * files could not be converted in one change without a way to see the result,
 * and a fabricated reason per file would have made the four real reasons above
 * unreadable.
 *
 * Sorted by size, which is also roughly the order to pay them off in: the top
 * of this list is where the type scale is most duplicated.
 */
/**
 * The ratchet. File -> the number of offending rules it held when this gate
 * landed (2026-09-10). The number may go DOWN and never up; the gate fails
 * both on a file that grows past its number and on a file that shrank without
 * its number being lowered in the same commit, so paying debt down is recorded
 * rather than quietly banked.
 *
 * These are NOT justified. Each one is a rule that should become a block, and
 * the reason it is a number instead of a fix is in the header: the four
 * surfaces held 264 such rules in 68 files on the day this was written, and a
 * fabricated per-file reason would have made the four real reasons above
 * unreadable.
 *
 * Sorted by size, which is roughly the order to pay them off in: the top of
 * this list is where the type scale is most duplicated.
 */
const PENDING_ROWS = [
  ['dashboard/src/routes/(public)/[user]/+page.svelte', 14],
  ['dashboard/src/routes/(public)/stats/+page.svelte', 11],
  ['dashboard/src/routes/(app)/counters/+page.svelte', 8],
  ['dashboard/src/routes/(public)/user/[channel]/+page.svelte', 6],
  ['dashboard/src/routes/(app)/settings/import/+page.svelte', 5],
  ['dashboard/src/lib/components/modules/TriggerRuleEditor.svelte', 5],
  ['dashboard/src/routes/(app)/settings/+page.svelte', 4],
  ['dashboard/src/routes/(app)/quotes/+page.svelte', 4],
  ['dashboard/src/lib/components/commands/CommandRow.svelte', 4],
  ['marketing/src/components/pricing/Tiers.astro', 3],
  ['marketing/src/components/changelog/ChangelogList.astro', 3],
  ['dashboard/src/routes/(app)/modules/+page.svelte', 3],
  ['dashboard/src/lib/components/commands/fetches/FetchKeyManager.svelte', 3],
  ['dashboard/src/lib/components/commands/CommandEditor.svelte', 3],
  ['dashboard/src/lib/components/channelpoints/RewardEditor.svelte', 3],
  ['dashboard/src/lib/components/OnboardingGuide.svelte', 3],
  ['marketing/src/components/pricing/Faq.astro', 2],
  ['marketing/src/components/home/SafetyLayers.astro', 2],
  ['marketing/src/components/home/EcoFriendly.astro', 2],
  ['marketing/src/components/guides/widgets/Rehearsal.astro', 2],
  ['marketing/src/components/guides/widgets/ModuleCatalog.astro', 2],
  ['dashboard/src/routes/(app)/songqueue/+page.svelte', 2],
  ['dashboard/src/routes/(app)/loyalty/+page.svelte', 2],
  ['dashboard/src/lib/components/overview/ActivityLog.svelte', 2],
  ['dashboard/src/lib/components/modules/ReplyEditor.svelte', 2],
  ['dashboard/src/lib/components/discord/GuildHeader.svelte', 2],
  ['dashboard/src/lib/components/commands/fetches/FetchSourcePicker.svelte', 2],
  ['dashboard/src/lib/components/commands/BuiltinInspector.svelte', 2],
  ['admin/src/lib/components/overview/BotCard.svelte', 2],
  ['marketing/src/pages/[...lang]/guides/index.astro', 1],
  ['marketing/src/layouts/Layout.astro', 1],
  ['marketing/src/components/home/Playground.astro', 1],
  ['marketing/src/components/guides/widgets/FetchOutcomes.astro', 1],
  ['marketing/src/components/guides/widgets/CounterPlay.astro', 1],
  ['marketing/src/components/guides/widgets/Checklist.astro', 1],
  ['marketing/src/components/guides/ChatMock.astro', 1],
  ['dashboard/src/routes/(app)/govee/+page.svelte', 1],
  ['dashboard/src/routes/(app)/discord/+page.svelte', 1],
  ['dashboard/src/routes/(app)/channelpoints/+page.svelte', 1],
  ['dashboard/src/routes/(app)/billing/+page.svelte', 1],
  ['dashboard/src/routes/(app)/+page.svelte', 1],
  ['dashboard/src/lib/components/spotify/SpotifyRewardEditor.svelte', 1],
  ['dashboard/src/lib/components/modules/ModuleCommandRow.svelte', 1],
  ['dashboard/src/lib/components/modules/ModuleCommandList.svelte', 1],
  ['dashboard/src/lib/components/govee/GoveeRewardEditor.svelte', 1],
  ['dashboard/src/lib/components/commands/AliasChips.svelte', 1],
  ['dashboard/src/lib/components/InstallAppPrompt.svelte', 1],
  ['admin/src/lib/components/users/MessageDialog.svelte', 1],
];

/**
 * Declarations that make a bare-element rule a restatement of the type scale
 * rather than page layout. `color` is in the list and `background` is not:
 * text colour is part of the scale (`Text tone`), a background is a surface.
 */
const TYPE_DECL =
  /(^|[\s;{])(font|font-size|font-family|font-weight|line-height|letter-spacing|text-transform|color)\s*:/;

/**
 * Files whose offending rules are counted, not forgiven: the number is what
 * the file had when this gate landed and may only go down. See the header.
 */
const PENDING = new Map(PENDING_ROWS);

const STYLE_BLOCK = /<style\b[^>]*>([\s\S]*?)<\/style>/g;

/**
 * Strip comments, keeping the newlines so reported line numbers stay true.
 * Half the "offenders" in a first draft of this gate were prose in a comment
 * that happened to start with `a ` or `button `.
 */
const stripComments = (css) =>
  css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));

/** The declaration block starting at `start`, up to its matching brace. */
function readBlock(css, start) {
  let depth = 1;
  let body = '';
  for (let i = start; i < css.length && depth > 0; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}') depth--;
    if (depth > 0) body += css[i];
  }
  return body;
}

/**
 * Selector heads inside a style block: the text before each `{` that is not an
 * at-rule prelude and not a declaration.
 *
 * A regex and not a CSS parser, for the same reason assert-ui-only-in-ui.mjs
 * greps for `<style` rather than compiling the component: this runs in the
 * same gate as the tests and has to stay boring. It costs two known
 * limitations, both accepted: a selector inside a string literal in a
 * `content:` value would be a false positive (never seen in this tree), and a
 * rule generated at runtime is invisible (as it is to every other lint here).
 */
/**
 * One step of the scan, per structural token. Split out of `rules` so the
 * generator is a loop and nothing else: the boundary handling is four guard
 * clauses here rather than four nested branches there.
 */
function advance(css, match, scan) {
  const token = match[0];
  const at = match.index;
  if (token === '\n') {
    scan.line++;
    return null;
  }
  // A `}` closes a block; a `;` ends a declaration. Either way what has
  // accumulated since the last boundary is not a selector.
  if (token !== '{') {
    scan.depth -= token === '}' ? 1 : 0;
    scan.headStart = at + 1;
    return null;
  }
  scan.depth++;
  const selector = css.slice(scan.headStart, at).trim();
  scan.headStart = at + 1;
  if (!selector || selector.startsWith('@')) return null;
  return { selector, line: scan.line, body: readBlock(css, at + 1) };
}

function* rules(css) {
  const clean = stripComments(css);
  const scan = { line: 1, headStart: 0, depth: 0 };
  for (const match of clean.matchAll(/[{};\n]/g)) {
    const hit = advance(clean, match, scan);
    if (hit) yield hit;
  }
}

/**
 * Does this selector target a bare element or a contract class?
 *
 * `:global(…)` is unwrapped rather than skipped: a `:global(.bb-chip)` reach-in
 * from a page route is the single most common form of the second shape, and it
 * is the one a scoped-styles reader is least likely to notice.
 */
function offendingParts(selector, body) {
  const flat = selector.replace(/:global\(([^)]*)\)/g, ' $1 ');
  const hits = new Set();
  for (const cls of flat.matchAll(/\.(bb-[\w-]+)/g)) hits.add(`.${cls[1]}`);
  // A bare type selector: an identifier not preceded by `.`, `#`, `-`, `%`,
  // `@` or a word character, and not inside an attribute selector's value.
  // A bare element only counts when the rule restates the TYPE SCALE. One that
  // merely places the element is this page's own layout; see the header.
  if (TYPE_DECL.test(body)) {
    const withoutAttrs = flat.replace(/\[[^\]]*\]/g, ' ');
    for (const match of withoutAttrs.matchAll(/(^|[\s>+~,()])([a-z][a-z0-9]*)\b/g)) {
      if (ELEMENTS.includes(match[2])) hits.add(match[2]);
    }
  }
  return [...hits];
}

async function* walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || entry.name === '.astro') continue;
      yield* walk(full);
      continue;
    }
    if (entry.name.endsWith('.svelte') || entry.name.endsWith('.astro')) yield full;
  }
}

const byFile = new Map();
let scanned = 0;

for (const surface of SURFACES) {
  const dir = join(webRoot, surface);
  for await (const file of walk(dir)) {
    const rel = relative(webRoot, file);
    if (ALLOWLIST.has(rel)) continue;
    const source = await readFile(file, 'utf8');
    scanned++;
    for (const block of source.matchAll(STYLE_BLOCK)) {
      const before = source.slice(0, block.index).split('\n').length - 1;
      for (const { selector, line, body } of rules(block[1])) {
        const parts = offendingParts(selector, body);
        if (!parts.length) continue;
        if (!byFile.has(rel)) byFile.set(rel, []);
        byFile.get(rel).push({
          line: before + line,
          selector: selector.replace(/\s+/g, ' '),
          parts,
        });
      }
    }
  }
}

const offenders = [];
const paid = [];
let debt = 0;

for (const [rel, found] of byFile) {
  const budget = PENDING.get(rel) ?? 0;
  debt += Math.min(found.length, budget);
  if (found.length <= budget) continue;
  // Only the rules over budget are reported: a pending file with one new rule
  // should point at the new one, not reprint the twelve that were already
  // counted. Which of them is "new" is not knowable from the source, so the
  // overflow is reported from the end of the file, and the message says so.
  for (const hit of found.slice(budget)) offenders.push({ rel, ...hit, budget, found: found.length });
}

for (const [rel, budget] of PENDING) {
  const now = byFile.get(rel)?.length ?? 0;
  if (now < budget) paid.push(`${rel}: ${budget} -> ${now}`);
}

if (paid.length > 0) {
  console.error(
    'assert-blocks-not-handcoded: debt was paid down and the ratchet was not\n' +
      'lowered with it. Update these PENDING numbers in\n' +
      'web/kit/scripts/assert-blocks-not-handcoded.mjs in the same commit:\n',
  );
  for (const p of paid) console.error(`  ${p}`);
  process.exit(1);
}

if (offenders.length > 0) {
  console.error(
    'assert-blocks-not-handcoded: hand-coded presentation found.\n' +
      'A rule that targets a bare element is a second answer to "what does a\n' +
      'heading look like"; a rule that targets a .bb-* class reaches into a\n' +
      'contract this file does not own. Render the block instead (Heading, Text,\n' +
      'Button, Field, Container, Stack, Cluster, Grid, Table …), or add a\n' +
      'modifier to the contract in ui/styles/elements/.\n',
  );
  for (const o of offenders) {
    console.error(
      `  ${o.rel}:${o.line}  ${o.selector}   [${o.parts.join(' ')}]` +
        (o.budget ? `   (file allowed ${o.budget}, has ${o.found})` : ''),
    );
  }
  console.error(
    `\n${offenders.length} rule${offenders.length === 1 ? '' : 's'} across ${
      new Set(offenders.map((o) => o.rel)).size
    } file${new Set(offenders.map((o) => o.rel)).size === 1 ? '' : 's'}.` +
      `\nAllowlisted, with the reason, in web/kit/scripts/assert-blocks-not-handcoded.mjs:` +
      `\n${[...ALLOWLIST.entries()].map(([k, why]) => `  ${k}\n    ${why}`).join('\n')}`,
  );
  process.exit(1);
}

console.log(
  `assert-blocks-not-handcoded: OK (${scanned} components scanned, ` +
    `${ALLOWLIST.size} allowlisted, ${debt} rules of counted debt in ${PENDING.size} files)`,
);
