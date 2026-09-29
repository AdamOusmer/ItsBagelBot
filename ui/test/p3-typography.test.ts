// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, describe, expect, test } from 'bun:test';
import { copyFileSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import SvelteText from '../svelte/Text.svelte';
import AstroText from '../astro/Text.astro';
import SvelteEyebrow from '../svelte/Eyebrow.svelte';
import AstroEyebrow from '../astro/Eyebrow.astro';
import SvelteHeading from '../svelte/Heading.svelte';
import AstroHeading from '../astro/Heading.astro';
import SvelteCode from '../svelte/Code.svelte';
import AstroCode from '../astro/Code.astro';
import SvelteAlertBanner from '../svelte/AlertBanner.svelte';
import AstroAlertBanner from '../astro/AlertBanner.astro';
import SvelteTag from '../svelte/Tag.svelte';
import AstroTag from '../astro/Tag.astro';
import SvelteBadge from '../svelte/Badge.svelte';
import AstroBadge from '../astro/Badge.astro';
import SvelteTable from '../svelte/Table.svelte';
import AstroTable from '../astro/Table.astro';

type Adapter = unknown;
type Props = Record<string, unknown>;

const snippet = (html: string) => createRawSnippet(() => ({ render: () => html }));

function svelteHtml(component: Adapter, props: Props, body: string): string {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return normalise(render(component as any, { props: { ...props, children: snippet(body) } }).body);
}

async function astroHtml(component: Adapter, props: Props, body: string): Promise<string> {
  const container = await experimental_AstroContainer.create();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return normalise(await container.renderToString(component as any, { props, slots: { default: body } }));
}

interface Case {
  name: string;
  svelte: Adapter;
  astro: Adapter;
  props: Props;
  body: string;
  html: string;
}

function contract({ name, svelte, astro, props, body, html }: Case) {
  test(name, async () => {
    const fromSvelte = svelteHtml(svelte, props, body);
    expect(fromSvelte).toBe(await astroHtml(astro, props, body));
    expect(fromSvelte).toBe(html);
  });
}

const css = (path: string) => readFileSync(new URL(`../styles/${path}`, import.meta.url), 'utf8');

describe('Text tones and truncate', () => {
  for (const tone of ['muted-light', 'muted-soft', 'soft', 'positive', 'warn']) {
    contract({
      name: `tone ${tone}`,
      svelte: SvelteText,
      astro: AstroText,
      props: { tone, size: 'sm' },
      body: 'Copy',
      html: `<p class="bb-text bb-text--sm bb-text--${tone}">Copy</p>`,
    });
  }

  contract({
    name: 'truncate sits after mono and before the caller class',
    svelte: SvelteText,
    astro: AstroText,
    props: { as: 'span', size: 'sm', mono: true, truncate: true, class: 'cell' },
    body: 'A long response',
    html: '<span class="bb-text bb-text--sm bb-text--mono bb-text--truncate cell">A long response</span>',
  });

  test('every tone is a token colour and warn has no literal', () => {
    const source = css('elements/typography.css');
    expect(source).toContain('.bb-text--muted-light { color: var(--bb-muted-light); }');
    expect(source).toContain('.bb-text--muted-soft { color: var(--bb-muted-soft); }');
    expect(source).toContain('.bb-text--positive { color: var(--bb-green-glow); }');
    expect(source).toContain('.bb-text--warn { color: var(--bb-warn); }');
    expect(source).not.toMatch(/#f2c879/i);
    expect(css('brand.css')).toContain('--bb-warn: #f2c879;');
  });

  test('truncate wins over the base wrap rule', () => {
    const source = css('elements/typography.css');
    const truncate = source.indexOf('.bb-text--truncate {');
    expect(truncate).toBeGreaterThan(source.indexOf('.bb-text {'));
    expect(source.slice(truncate, source.indexOf('}', truncate))).toContain('white-space: nowrap;');
  });
});

describe('Eyebrow tone', () => {
  contract({
    name: 'default stays tan with no modifier',
    svelte: SvelteEyebrow,
    astro: AstroEyebrow,
    props: {},
    body: 'Live',
    html: '<span class="bb-eyebrow">Live</span>',
  });

  contract({
    name: 'go tone',
    svelte: SvelteEyebrow,
    astro: AstroEyebrow,
    props: { tone: 'go', as: 'p' },
    body: 'Live',
    html: '<p class="bb-eyebrow bb-eyebrow--go">Live</p>',
  });
});

describe('Heading variants', () => {
  contract({
    name: 'title variant keeps the level element',
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: { level: 5, as: 'h3', variant: 'title' },
    body: 'Timers',
    html: '<h3 class="bb-h bb-h--l5 bb-h--title">Timers</h3>',
  });

  contract({
    name: 'label variant on a real heading',
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: { level: 3, variant: 'label' },
    body: 'History',
    html: '<h3 class="bb-h bb-h--l3 bb-h--label">History</h3>',
  });

  contract({
    name: 'uppercase follows the variant',
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: { level: 5, as: 'h3', variant: 'title', uppercase: true, class: 'tier__name' },
    body: 'Free',
    html: '<h3 class="bb-h bb-h--l5 bb-h--title bb-h--upper tier__name">Free</h3>',
  });

  test('label variant reproduces the admin block label', () => {
    const source = css('elements/typography.css');
    const start = source.indexOf('.bb-h--label {');
    const rule = source.slice(start, source.indexOf('}', start));
    for (const line of [
      'font-family: var(--bb-font-mono);',
      'font-size: 10px;',
      'letter-spacing: 0.12em;',
      'text-transform: uppercase;',
      'color: var(--bb-muted);',
    ]) {
      expect(rule).toContain(line);
    }
  });

  test('programmatic focus drops the outline and keyboard focus keeps it', () => {
    const source = css('elements/typography.css');
    expect(source).toContain('.bb-h:focus:not(:focus-visible) { outline: none; }');
    expect(source).not.toMatch(/\.bb-h:focus-visible[^{]*\{[^}]*outline:\s*none/);
  });
});

describe('Code options', () => {
  contract({
    name: 'positive inline tone',
    svelte: SvelteCode,
    astro: AstroCode,
    props: { tone: 'positive' },
    body: '{counter:target:deaths}',
    html: '<code class="bb-code bb-code--positive">{counter:target:deaths}</code>',
  });

  contract({
    name: 'wrapping block with a max height',
    svelte: SvelteCode,
    astro: AstroCode,
    props: { block: true, wrap: true, maxHeight: '150px', 'data-output': '' },
    body: '!hello',
    html: '<pre class="bb-code-block bb-code-block--wrap" style="max-height:150px" data-output><code class="bb-code">!hello</code></pre>',
  });

  contract({
    name: 'plain block is unchanged',
    svelte: SvelteCode,
    astro: AstroCode,
    props: { block: true },
    body: 'x',
    html: '<pre class="bb-code-block"><code class="bb-code">x</code></pre>',
  });
});

describe('AlertBanner options', () => {
  contract({
    name: 'positive callout',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { variant: 'positive', callout: true, role: 'note' },
    body: '<b>In plain words</b>We keep nothing.',
    html: '<div class="bb-alert bb-alert--positive bb-alert--callout" role="note"><span class="bb-alert__msg"><b>In plain words</b>We keep nothing.</span></div>',
  });

  contract({
    name: 'flush warn row',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { variant: 'warn', flush: true, role: 'note' },
    body: 'Two commands are off.',
    html: '<div class="bb-alert bb-alert--warn bb-alert--flush" role="note"><span class="bb-alert__msg">Two commands are off.</span></div>',
  });

  test('flush comes after callout so it clears both margins', () => {
    const source = css('elements/alert.css');
    expect(source.indexOf('.bb-alert--flush {')).toBeGreaterThan(source.indexOf('.bb-alert--callout {'));
    expect(source).toMatch(/\.bb-alert--positive \{[^}]*--alert-rgb: var\(--bb-green-glow-rgb\)/);
  });

  contract({
    name: 'stacked row with an action',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { variant: 'danger', role: 'note', stack: true },
    body: 'Two conflicts.',
    html: '<div class="bb-alert bb-alert--danger bb-alert--stack" role="note"><span class="bb-alert__msg">Two conflicts.</span></div>',
  });

  test('stack gives the message its own row on phones so the action drops below it', () => {
    const source = css('elements/alert.css');
    expect(source).toMatch(
      /@media \(max-width: 560px\) \{\s*\.bb-alert--stack \{\s*flex-wrap: wrap;\s*\}\s*\.bb-alert--stack \.bb-alert__msg \{\s*flex-basis: 100%;/,
    );
  });
});

describe('Tag options', () => {
  contract({
    name: 'info tone',
    svelte: SvelteTag,
    astro: AstroTag,
    props: { tone: 'info' },
    body: 'Event',
    html: '<span class="bb-tag bb-tag--info">Event</span>',
  });

  contract({
    name: 'bare together with a tone',
    svelte: SvelteTag,
    astro: AstroTag,
    props: { tone: 'live', bare: true, class: 'chat-tag' },
    body: 'Rehearsal',
    html: '<span class="bb-tag bb-tag--live bb-tag--bare chat-tag">Rehearsal</span>',
  });

  contract({
    name: 'literal command chip',
    svelte: SvelteTag,
    astro: AstroTag,
    props: { tone: 'bare', literal: true },
    body: '!so',
    html: '<span class="bb-tag bb-tag--bare bb-tag--literal">!so</span>',
  });

  test('info joins the toned group', () => {
    const source = css('tags.css');
    expect(source).toContain('.bb-tag--info');
    expect(source).toMatch(/\.bb-tag:is\([^)]*\.bb-tag--info\)/);
  });
});

describe('Badge options', () => {
  contract({
    name: 'existing class order is kept',
    svelte: SvelteBadge,
    astro: AstroBadge,
    props: { tone: 'free', dashed: true },
    body: 'Lead mod',
    html: '<span class="bb-tag bb-badge bb-tag--free bb-badge--dashed">Lead mod</span>',
  });

  contract({
    name: 'dashed literal pill',
    svelte: SvelteBadge,
    astro: AstroBadge,
    props: { shape: 'pill', tone: 'neutral', dashed: true, literal: true },
    body: 'No ads, ever.',
    html: '<span class="bb-tag bb-badge bb-badge--pill bb-tag--neutral bb-badge--dashed bb-tag--literal">No ads, ever.</span>',
  });

  test('dashed pill dashes every side', () => {
    expect(css('elements/badge.css')).toContain('.bb-badge--pill.bb-badge--dashed { border-style: dashed; }');
  });
});

describe('Table options', () => {
  contract({
    name: 'roomy table',
    svelte: SvelteTable,
    astro: AstroTable,
    props: { label: 'Compare', roomy: true },
    body: '<tbody><tr><td class="acc">Yes</td></tr></tbody>',
    html: '<div class="bb-tbl-wrap" role="region" aria-label="Compare" tabindex="0"><table class="bb-tbl bb-tbl--roomy"><tbody><tr><td class="acc">Yes</td></tr></tbody></table></div>',
  });

  test('accent cell and roomy padding are styled', () => {
    const source = css('elements/table.css');
    expect(source).toContain('.bb-tbl .acc { color: var(--bb-tan-light); }');
    expect(source).toMatch(/\.bb-tbl--roomy :is\(td, th\[scope="row"\]\) \{[^}]*padding: 12px 16px;/);
  });

  contract({
    name: 'minimum width makes the wrapper scroll',
    svelte: SvelteTable,
    astro: AstroTable,
    props: { label: 'Winners', minWidth: '980px' },
    body: '<tbody><tr><td>1</td></tr></tbody>',
    html: '<div class="bb-tbl-wrap" role="region" aria-label="Winners" tabindex="0"><table class="bb-tbl" style="--tbl-min-w: 980px;"><tbody><tr><td>1</td></tr></tbody></table></div>',
  });

  test('the table floor reads its hook and stays auto without it', () => {
    expect(css('elements/table.css')).toMatch(/\.bb-tbl \{[^}]*min-width: var\(--tbl-min-w, auto\);/);
  });
});

describe('Modal viewer light theme', () => {
  test('viewer chrome has light rules on paper and ink tokens', () => {
    const source = css('elements/modal.css');
    expect(source).toMatch(/:root\[data-theme="light"\] \.bb-modal--viewer \.bb-modal__backdrop \{[^}]*--bb-paper-warm-rgb/);
    expect(source).toMatch(/:root\[data-theme="light"\] \.bb-modal__toolbar \.bb-btn--icon \{[^}]*--bb-ink-soft/);
    expect(source).toMatch(/:root\[data-theme="light"\] \.bb-modal__hint \{[^}]*--bb-ink-rgb/);
  });
});

const savedGlobals = new Map(
  ['requestAnimationFrame', 'cancelAnimationFrame', 'IntersectionObserver', 'Element'].map(
    (key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const,
  ),
);
const isolatedDir = mkdtempSync(join(tmpdir(), 'bagel-decode-test-'));
afterAll(() => {
  for (const [key, descriptor] of savedGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
  rmSync(isolatedDir, { recursive: true, force: true });
});

let queue: FrameRequestCallback[] = [];
globalThis.requestAnimationFrame = ((callback: FrameRequestCallback) => {
  queue.push(callback);
  return queue.length;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = (() => {
  queue = [];
}) as typeof cancelAnimationFrame;

function frame(time: number): void {
  const pending = queue;
  queue = [];
  for (const callback of pending) callback(time);
}

for (const file of ['decode.ts', 'raf-loop.ts', 'motion-query.ts']) {
  copyFileSync(new URL(`../lib/${file}`, import.meta.url), join(isolatedDir, file));
}
const { decode, observeDecode } = (await import(join(isolatedDir, 'decode.ts'))) as typeof import('../lib/decode');

const target = () => ({ textContent: '' }) as unknown as HTMLElement;

describe('decode', () => {
  test('one-shot scramble uses the given charset and duration, then settles', () => {
    const el = target();
    const t0 = performance.now();
    decode(el, 'moth_lamp', { charset: 'x', durationMs: 900 });
    frame(t0);
    expect(el.textContent).toBe('xxxxxxxxx');
    frame(t0 + 450);
    expect(el.textContent?.startsWith('moth')).toBe(true);
    frame(t0 + 905);
    expect(el.textContent).toBe('moth_lamp');
    expect(queue).toHaveLength(0);
  });

  test('default duration still follows text length', () => {
    const el = target();
    const t0 = performance.now();
    decode(el, 'abcdefghij', {});
    frame(t0 + 639);
    expect(el.textContent).not.toBe('abcdefghij');
    frame(t0 + 645);
    expect(el.textContent).toBe('abcdefghij');
  });

  test('stopping early leaves the frame where it was', () => {
    const el = target();
    const t0 = performance.now();
    const stop = decode(el, 'gg beans', { charset: '#' });
    frame(t0);
    stop();
    frame(t0 + 2000);
    expect(el.textContent).toBe('## #####');
  });

  test('observeDecode passes scramble options through', () => {
    class FakeElement {}
    globalThis.Element = FakeElement as unknown as typeof Element;
    globalThis.IntersectionObserver = class {
      constructor(private readonly callback: IntersectionObserverCallback) {}
      observe(el: Element) {
        this.callback([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as never);
      }
      unobserve() {}
      disconnect() {}
    } as unknown as typeof IntersectionObserver;

    const el = { textContent: 'tiny', dataset: { decode: 'soupdream' } } as unknown as HTMLElement;
    const root = { querySelectorAll: () => [el] } as unknown as ParentNode;
    const t0 = performance.now();
    const stop = observeDecode(root, { charset: 'z', durationMs: 900 });
    frame(t0);
    expect(el.textContent).toBe('zzzzzzzzz');
    stop();
  });
});
