#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Presentation guard for @bagel/kit, shaped like ./assert-demo-gated.mjs: a
// source-level grep test that names the file and the line rather than a rule
// nobody runs.
//
// THE RULE. No `<style>` block in web/kit/components/*.svelte.
//
// WHY IT IS A GATE AND NOT A CONVENTION. The whole argument for a standalone
// @bagel/ui is that one element has one contract, in one file, that every
// surface reads. A `<style>` block in a kit component is a second answer to
// "what does this look like", scoped so it always wins locally and invisible
// from the design library — which is precisely how the duplication this stack
// exists to delete was created the first time: a tag/chip block that existed
// twice, a button contract that existed three times, four nav links with four
// different hover motions. None of those were written by someone ignoring a
// rule. They were written by someone styling the component in front of them.
//
// After this gate, kit components are WRAPPERS: they bind bot data or a server
// form action and render a ui element. If a wrapper needs to look like
// something, that something is an element, and it belongs in ui.
//
// THE ALLOWLIST. Kept deliberately small; a wrapper earns a row only when its
// layout glue is genuinely about the DATA it binds and would be meaningless in
// a design library. Every entry carries the reason inline. Adding one is a
// review conversation, not an edit: the cheap answer is nearly always "the
// element this wraps grows a modifier".
//
// THE SKIP LIST. `--skip=A,B` (or KIT_UI_ONLY_SKIP=A,B) suppresses named
// components while a branch of the element migration is still in flight: on
// any single branch, the components ANOTHER branch is moving are still here
// with their styles intact.
//
// There is no longer a written-down one. `PENDING_STACK` used to hold the
// thirty components the button/card, field/toggle/badge and nav/shell branches
// owned; all three have landed under this one, so the constant is deleted and
// the gate runs unconditional by default. It is left as a flag rather than
// removed outright because the next element stack will want it, and because a
// skip passed on the command line is visible in the CI log while a constant
// is not.
//
// A skipped component is not an allowed one. The list is printed on every run,
// so it is a visible debt rather than a silent exemption. A permanent skip is
// an allowlist entry that skipped the review.
//
// What it cannot enforce: a wrapper that writes presentation as inline
// `style="…"`, or one that reaches into a ui element with a `:global` selector
// from a parent app route. Those are review's job. This catches the shape that
// actually happens.

import { readdir, readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

const kitRoot = resolve(import.meta.dir, '..');
const componentsDir = join(kitRoot, 'components');

/**
 * Components allowed to keep a `<style>` block, with the reason. Each of these
 * binds bot data or a SvelteKit form action and the rules it keeps position
 * that binding rather than describing an element.
 */
const ALLOWLIST = new Map([
  [
    'AccountFoot.svelte',
    // The rail's footer slot: it positions the session menu against the rail's
    // own width and collapsed state, which are the shell's geometry and not
    // this component's. The menu itself is ui.
    'positions the session menu inside the rail slot it is given',
  ],
  [
    'Bolota.svelte',
    // Not a ui element and deliberately not becoming one (user decision,
    // 2026-09-09): it is a thin binding over @luzir/bolota, a third-party
    // avatar engine that ships its own `.bolota` class namespace and its own
    // package. The one rule kept here (`display: block; overflow: visible`)
    // undoes the inline-SVG default for the library's root, which is the
    // library's business rather than this design system's. Taking
    // @luzir/bolota as a @bagel/ui dependency was the alternative and was
    // rejected: it would make a bot-specific avatar renderer a dependency of
    // every consumer of the design library.
    'wraps the third-party @luzir/bolota engine and its own class namespace',
  ],
  [
    'NavItem.svelte',
    // The rail's ledger entry. Everything that was LINK styling here moved to
    // @bagel/ui/styles/elements/nav-link.css; the two rules left describe the
    // registry's own columns -- the entry's index and its live count -- which
    // are what make the rail a numbered register rather than a menu. No other
    // surface that renders a nav link has either, so a `.bb-nav-link--ledger`
    // modifier in the library would be a modifier with one caller, named after
    // this wrapper. Same shape as NotificationBell below: a live number pinned
    // to a control the library drew.
    'positions the nav registry index and live count inside the link it renders',
  ],
  [
    'NotificationBell.svelte',
    // The unread count is absolutely positioned against the bell trigger, and
    // its offset depends on the live count's digit width. A ui Badge cannot
    // know that it is being pinned to a bell.
    'pins the live unread count to its trigger',
  ],
  [
    'OperatorMenu.svelte',
    // Same shape as AccountFoot: anchors a session popover to the topbar.
    'anchors the session popover to the topbar',
  ],
  [
    'RootShell.svelte',
    // The app-level stacking context and the skip-link landing box. Layout of
    // the whole document, which is the one thing that is not an element.
    'owns the app-level stacking context',
  ],
]);

/** Component files, sorted so the output is stable. */
async function componentFiles() {
  const entries = await readdir(componentsDir, { withFileTypes: true });
  return entries
    .filter((e) => e.isFile() && e.name.endsWith('.svelte'))
    .map((e) => e.name)
    .sort();
}

// Matched against the raw source. A `<style>` inside a comment or a string
// would be a false positive; neither has ever appeared in this tree, and the
// alternative (parsing every component with the Svelte compiler to ask the same
// question) costs a compiler dependency in a gate that has to stay boring.
const STYLE_BLOCK = /^\s*<style\b/m;

function parseSkips() {
  const fromArgs = process.argv
    .slice(2)
    .filter((a) => a.startsWith('--skip='))
    .flatMap((a) => a.slice('--skip='.length).split(','));
  const fromEnv = (process.env.KIT_UI_ONLY_SKIP ?? '').split(',');
  return new Set(
    [...fromArgs, ...fromEnv]
      .map((s) => s.trim())
      .filter(Boolean)
      .map((s) => (s.endsWith('.svelte') ? s : `${s}.svelte`)),
  );
}

const skips = parseSkips();
const offenders = [];
const skipped = [];

for (const name of await componentFiles()) {
  if (ALLOWLIST.has(name)) continue;
  const source = await readFile(join(componentsDir, name), 'utf8');
  if (!STYLE_BLOCK.test(source)) continue;

  const line = source.split('\n').findIndex((l) => /^\s*<style\b/.test(l)) + 1;
  if (skips.has(name)) {
    skipped.push(`${name}:${line}`);
    continue;
  }
  offenders.push(`web/kit/components/${name}:${line}`);
}

if (skipped.length > 0) {
  console.log(
    `assert-ui-only-in-ui: ${skipped.length} still carrying presentation, owned by an unlanded branch of the element stack: ${skipped.join(', ')}`,
  );
}

if (offenders.length > 0) {
  console.error(
    'assert-ui-only-in-ui: presentation found in a kit component.\n' +
      'A kit component binds data and renders a @bagel/ui element; what it looks\n' +
      'like belongs in ui/styles/elements/<name>.css with an adapter beside it.\n',
  );
  for (const o of offenders) console.error(`  ${o}  <style> block`);
  console.error(
    `\n${offenders.length} component${offenders.length === 1 ? '' : 's'} with a <style> block.` +
      `\nAllowlisted, with the reason, in ${'web/kit/scripts/assert-ui-only-in-ui.mjs'}:` +
      `\n${[...ALLOWLIST.entries()].map(([k, why]) => `  ${k} — ${why}`).join('\n')}`,
  );
  process.exit(1);
}

console.log(
  `assert-ui-only-in-ui: OK (${ALLOWLIST.size} allowlisted, ${skipped.length} skipped)`,
);
