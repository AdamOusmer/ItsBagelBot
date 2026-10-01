// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe } from 'bun:test';
import { contracts, sourceContracts } from './contract';
import SvelteCard from '../svelte/Card.svelte';
import AstroCard from '../astro/Card.astro';
import SvelteDeckLayout from '../svelte/DeckLayout.svelte';
import AstroDeckLayout from '../astro/DeckLayout.astro';
import SvelteDisclosure from '../svelte/Disclosure.svelte';
import AstroDisclosure from '../astro/Disclosure.astro';
import SvelteFact from '../svelte/Fact.svelte';
import AstroFact from '../astro/Fact.astro';
import SvelteFactList from '../svelte/FactList.svelte';
import AstroFactList from '../astro/FactList.astro';
import SvelteManagementRow from '../svelte/ManagementRow.svelte';
import AstroManagementRow from '../astro/ManagementRow.astro';
import SveltePager from '../svelte/Pager.svelte';
import AstroPager from '../astro/Pager.astro';
import SvelteProgressBar from '../svelte/ProgressBar.svelte';
import AstroProgressBar from '../astro/ProgressBar.astro';
import SvelteSection from '../svelte/Section.svelte';
import AstroSection from '../astro/Section.astro';
import SvelteStatTile from '../svelte/StatTile.svelte';
import AstroStatTile from '../astro/StatTile.astro';
import SvelteTable from '../svelte/Table.svelte';
import AstroTable from '../astro/Table.astro';

describe('DeckLayout', () => {
  contracts(SvelteDeckLayout, AstroDeckLayout, [
    {
      name: 'idle deck is one column',
      slots: { default: '<p>list</p>' },
      html: '<div class="bb-deck-layout"><p>list</p></div>',
    },
    {
      name: 'inspecting with the admin column width',
      props: { inspecting: true, width: '380px', class: 'users' },
      slots: { default: '<p>list</p><aside>inspector</aside>' },
      html:
        '<div class="bb-deck-layout is-inspecting users" style="--deck-aside: 380px;">' +
        '<p>list</p><aside>inspector</aside></div>',
    },
  ]);
});

describe('ManagementRow modes', () => {
  contracts(
    SvelteManagementRow,
    AstroManagementRow,
    [
      {
        name: 'href renders an anchor primary',
        props: { href: '/modules/quotes', label: 'Open Quotes', selected: true },
        slots: { default: 'Quotes' },
        html:
          '<div class="bb-row row-shell is-selected">' +
          '<a class="bb-row__primary" href="/modules/quotes" data-cursor="quiet" aria-current="true" aria-label="Open Quotes">Quotes</a>' +
          '</div>',
      },
      {
        name: 'non-selectable row is static',
        props: { selectable: false, as: 'article' },
        slots: { default: 'shard 0', actions: '<i>m</i>' },
        html:
          '<article class="bb-row row-shell bb-row--static">' +
          '<div class="bb-row__primary" data-cursor="quiet">shard 0</div>' +
          '<div class="bb-row__actions"><i>m</i></div></article>',
      },
      {
        name: 'title, meta and marks render the shared row line',
        props: { title: 'ada', meta: 'id 4', controls: 'insp' },
        slots: { leading: '<b>L</b>', badge: '<b>You</b>', default: '<b>meter</b>', marks: '<b>paid</b>' },
        html:
          '<div class="bb-row row-shell">' +
          '<button class="bb-row__primary" type="button" data-cursor="quiet" aria-expanded="false" aria-controls="insp">' +
          '<span class="bb-row__line"><b>L</b><span class="bb-row__text">' +
          '<span class="bb-row__title">ada<span class="bb-row__badge"><b>You</b></span></span>' +
          '<span class="bb-row__meta">id 4</span></span><b>meter</b>' +
          '<span class="bb-row__marks"><b>paid</b></span></span></button></div>',
      },
      {
        name: 'title alone renders no empty slots',
        props: { title: 'lane' },
        html:
          '<div class="bb-row row-shell">' +
          '<button class="bb-row__primary" type="button" data-cursor="quiet" aria-expanded="false">' +
          '<span class="bb-row__line"><span class="bb-row__text"><span class="bb-row__title">lane</span></span></span>' +
          '</button></div>',
      },
      {
        name: 'wrapping title and stacked actions are opt-in modifiers',
        props: { selectable: false, wrap: true, stackActions: true, title: 'Ada Lovelace' },
        slots: { badge: '<b>You</b>', actions: '<i>go</i>' },
        html:
          '<div class="bb-row row-shell bb-row--static bb-row--wrap bb-row--stack-actions">' +
          '<div class="bb-row__primary" data-cursor="quiet"><span class="bb-row__line"><span class="bb-row__text">' +
          '<span class="bb-row__title">Ada Lovelace<span class="bb-row__badge"><b>You</b></span></span></span></span></div>' +
          '<div class="bb-row__actions"><i>go</i></div></div>',
      },
    ],
    'primary',
  );
});

describe('FactList and Fact', () => {
  contracts(SvelteFactList, AstroFactList, [
    {
      name: 'rows is the default layout',
      slots: { default: '<div>f</div>' },
      html: '<dl class="bb-facts"><div>f</div></dl>',
    },
    {
      name: 'tiles layout',
      props: { layout: 'tiles', class: 'review' },
      slots: { default: '<div>f</div>' },
      html: '<dl class="bb-facts bb-facts--tiles review"><div>f</div></dl>',
    },
    {
      name: 'hidden fact list',
      props: { layout: 'tiles', hidden: true },
      html: '<dl class="bb-facts bb-facts--tiles" hidden></dl>',
    },
  ]);

  contracts(SvelteFact, AstroFact, [
    {
      name: 'fact: plain term and value',
      props: { term: 'Stream' },
      slots: { default: 'BOTS' },
      html: '<div class="bb-fact"><dt class="bb-fact__term">Stream</dt><dd class="bb-fact__value">BOTS</dd></div>',
    },
    {
      name: 'fact: every modifier',
      props: { term: 'Key', tone: 'danger', mono: true, wide: true, truncate: true },
      slots: { default: 'missing' },
      html:
        '<div class="bb-fact bb-fact--danger bb-fact--mono bb-fact--wide bb-fact--truncate">' +
        '<dt class="bb-fact__term">Key</dt><dd class="bb-fact__value">missing</dd></div>',
    },
    {
      name: 'hidden fact',
      props: { term: 'Blocked', hidden: true },
      slots: { default: '3' },
      html: '<div class="bb-fact" hidden><dt class="bb-fact__term">Blocked</dt><dd class="bb-fact__value">3</dd></div>',
    },
  ]);
});

describe('StatTile', () => {
  contracts(SvelteStatTile, AstroStatTile, [
    {
      name: 'no delta row is rendered',
      props: { label: 'Eligible', value: '42' },
      html:
        '<div class="bb-stat"><div class="bb-stat__head"><span class="bb-stat__label">Eligible</span></div>' +
        '<div class="bb-stat__value"><span data-count-up>42</span></div></div>',
    },
    {
      name: 'flat delta still renders',
      props: { label: 'Fires', value: '9', delta: 'today', flat: true },
      html:
        '<div class="bb-stat"><div class="bb-stat__head"><span class="bb-stat__label">Fires</span></div>' +
        '<div class="bb-stat__value"><span data-count-up>9</span></div>' +
        '<div class="bb-stat__delta" data-flat>today</div></div>',
    },
    {
      name: 'static tile skips the count-up',
      props: { label: 'Commands', value: '12', static: true },
      html:
        '<div class="bb-stat bb-stat--static"><div class="bb-stat__head"><span class="bb-stat__label">Commands</span></div>' +
        '<div class="bb-stat__value"><span>12</span></div></div>',
    },
    {
      name: 'inline toned stat',
      props: { label: 'Answered', value: '1,204', static: true, inline: true, tone: 'success' },
      html:
        '<div class="bb-stat bb-stat--static bb-stat--inline bb-stat--success"><div class="bb-stat__head">' +
        '<span class="bb-stat__label">Answered</span></div><div class="bb-stat__value"><span>1,204</span></div></div>',
    },
  ]);
});

describe('Card and Section', () => {
  contracts(SvelteCard, AstroCard, [
    {
      name: 'flush accent card',
      props: { flush: true, tone: 'accent' },
      slots: { default: 'body' },
      html: '<div class="bb-card bb-card--flush bb-card--accent" data-card>body</div>',
    },
    {
      name: 'danger section',
      props: { as: 'section', tone: 'danger', id: 'danger-zone' },
      slots: { default: 'body' },
      html: '<section class="bb-card bb-card--danger" data-card id="danger-zone">body</section>',
    },
    {
      name: 'dashed card',
      props: { dashed: true, hidden: true },
      slots: { default: 'Generate' },
      html: '<div class="bb-card bb-card--dashed" data-card hidden>Generate</div>',
    },
  ]);

  contracts(SvelteSection, AstroSection, [
    {
      name: 'page rhythm',
      props: { size: 'page' },
      slots: { default: 'x' },
      html: '<section class="bb-section bb-section--page">x</section>',
    },
  ]);
});

describe('Pager', () => {
  contracts(SveltePager, AstroPager, [
    {
      name: 'first page disables previous',
      props: { label: 'Page 1 of 3', prevHref: '?page=0', nextHref: '?page=2', hasPrev: false },
      html:
        '<nav class="bb-pager" aria-label="Pagination">' +
        '<a class="bb-btn bb-btn--ghost" role="link" aria-disabled="true" data-mark>' +
        '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Previous</span></a>' +
        '<span class="bb-pager__label" aria-current="page">Page 1 of 3</span>' +
        '<a class="bb-btn bb-btn--ghost" href="?page=2" data-mark>' +
        '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Next</span></a></nav>',
    },
    {
      name: 'host labels replace the defaults',
      props: {
        label: 'Page 3 sur 3',
        prevHref: '?page=2',
        nextHref: '?page=4',
        hasNext: false,
        prevLabel: 'Précédent',
        nextLabel: 'Suivant',
        navLabel: 'Pages',
      },
      html:
        '<nav class="bb-pager" aria-label="Pages">' +
        '<a class="bb-btn bb-btn--ghost" href="?page=2" data-mark>' +
        '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Précédent</span></a>' +
        '<span class="bb-pager__label" aria-current="page">Page 3 sur 3</span>' +
        '<a class="bb-btn bb-btn--ghost" role="link" aria-disabled="true" data-mark>' +
        '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Suivant</span></a></nav>',
    },
  ]);
});

describe('Disclosure', () => {
  contracts(SvelteDisclosure, AstroDisclosure, [
    {
      name: 'indexed and open',
      props: { summary: 'Can I cancel?', index: '01', open: true },
      slots: { default: '<p>Yes.</p>' },
      html:
        '<details class="bb-disclosure bb-disclosure--indexed" open>' +
        '<summary class="bb-disclosure__summary"><span class="bb-disclosure__index" aria-hidden="true">01</span>' +
        '<span class="bb-disclosure__label">Can I cancel?</span>' +
        '<span class="bb-disclosure__icon" aria-hidden="true"></span></summary>' +
        '<div class="bb-disclosure__body"><div class="bb-disclosure__content"><p>Yes.</p></div></div></details>',
    },
    {
      name: 'compact and closed',
      props: { summary: '12 commits', size: 'sm' },
      slots: { default: '<ul></ul>' },
      html:
        '<details class="bb-disclosure bb-disclosure--sm">' +
        '<summary class="bb-disclosure__summary"><span class="bb-disclosure__label">12 commits</span>' +
        '<span class="bb-disclosure__icon" aria-hidden="true"></span></summary>' +
        '<div class="bb-disclosure__body"><div class="bb-disclosure__content"><ul></ul></div></div></details>',
    },
  ]);
});

const bar = (cls: string, aria: string, inner = '<span class="bb-progress__fill"></span>') =>
  `<div class="bb-progress ${cls}" role="progressbar" aria-label="Load" aria-valuemin="0" aria-valuemax="100" ${aria}>${inner}</div>`;

describe('ProgressBar', () => {
  contracts(SvelteProgressBar, AstroProgressBar, [
    {
      name: 'determinate: whole percent for screen readers, raw fraction for the fill',
      props: { value: 0.4666, tone: 'success', label: 'Load' },
      html: bar('bb-progress--success', 'aria-valuenow="47" style="--progress: 0.4666;"'),
    },
    {
      name: 'indeterminate: busy, no value, no --progress',
      props: { value: null, size: 'sm', label: 'Load' },
      html: bar('bb-progress--neutral bb-progress--sm bb-progress--indeterminate', 'aria-busy="true"'),
    },
    {
      name: 'over 1 clamps to full',
      props: { value: 1.7, tone: 'danger', label: 'Load' },
      html: bar('bb-progress--danger', 'aria-valuenow="100" style="--progress: 1;"'),
    },
    {
      name: 'NaN renders empty rather than aria-valuenow="NaN"',
      props: { value: Number.NaN, tone: 'warning', label: 'Load' },
      html: bar('bb-progress--warning', 'aria-valuenow="0" style="--progress: 0;"'),
    },
    {
      name: 'target marker is clamped and decorative',
      props: { value: 0.5, tone: 'warning', target: 1.4, label: 'Load' },
      html: bar(
        'bb-progress--warning bb-progress--target',
        'aria-valuenow="50" style="--progress: 0.5;"',
        '<span class="bb-progress__fill"></span><span class="bb-progress__target" aria-hidden="true" style="--target: 1;"></span>',
      ),
    },
    {
      name: 'segments replace the fill, one current',
      props: { value: 0.5, size: 'sm', segments: ['warning', 'success', null], current: 1, label: 'Load' },
      html: bar(
        'bb-progress--neutral bb-progress--sm bb-progress--segmented',
        'aria-valuenow="50" style="--progress: 0.5;"',
        '<span class="bb-progress__seg bb-progress__seg--warning"></span>' +
          '<span class="bb-progress__seg bb-progress__seg--success is-current"></span>' +
          '<span class="bb-progress__seg"></span>',
      ),
    },
    {
      name: 'gradient fill on a ramp step',
      props: { value: 0.5, tone: 'success', label: 'Load', gradient: true, ramp: 2 },
      html: bar(
        'bb-progress--success bb-progress--gradient bb-progress--ramp-2',
        'aria-valuenow="50" style="--progress: 0.5;"',
      ),
    },
    {
      name: 'ramp 1 is the tone itself',
      props: { value: 1, tone: 'success', label: 'Load', ramp: 1 },
      html: bar('bb-progress--success', 'aria-valuenow="100" style="--progress: 1;"'),
    },
  ]);
});

describe('Table', () => {
  contracts(SvelteTable, AstroTable, [
    {
      name: 'roomy table',
      props: { label: 'Compare', roomy: true },
      slots: { default: '<tbody><tr><td class="acc">Yes</td></tr></tbody>' },
      html:
        '<div class="bb-tbl-wrap" role="region" aria-label="Compare" tabindex="0"><table class="bb-tbl bb-tbl--roomy">' +
        '<tbody><tr><td class="acc">Yes</td></tr></tbody></table></div>',
    },
    {
      name: 'minimum width makes the wrapper scroll',
      props: { label: 'Winners', minWidth: '980px' },
      slots: { default: '<tbody><tr><td>1</td></tr></tbody>' },
      html:
        '<div class="bb-tbl-wrap" role="region" aria-label="Winners" tabindex="0"><table class="bb-tbl" style="--tbl-min-w: 980px;">' +
        '<tbody><tr><td>1</td></tr></tbody></table></div>',
    },
  ]);
});

const BRAND_TOKEN_FILES = [
  'card.css',
  'deck-layout.css',
  'deck-list.css',
  'disclosure.css',
  'fact-list.css',
  'feed.css',
  'management-row.css',
  'pager.css',
  'progress-bar.css',
];

describe('layout and data stylesheets', () => {
  sourceContracts([
    {
      name: 'wrap lets the title break beside its badge, and stacked actions indent on phones',
      checks: [
        {
          file: 'styles/elements/management-row.css',
          has: [
            /\.bb-row--wrap \.bb-row__title \{[^}]*display: flex;[^}]*overflow-wrap: anywhere;[^}]*white-space: normal;/,
            /\.bb-row--wrap \.bb-row__badge \{\s*flex: none;/,
            /@media \(max-width: 600px\) \{\s*\.bb-row--stack-actions \{\s*flex-wrap: wrap;\s*\}\s*\.bb-row--stack-actions \.bb-row__actions \{[^}]*flex-basis: 100%;[^}]*var\(--row-actions-indent, 14px\)/,
          ],
        },
      ],
    },
    {
      name: 'a deck list drops the list margin and markers when it renders as a list',
      checks: [{ file: 'styles/elements/deck-list.css', has: [/\.bb-deck-list \{[^}]*margin: 0;[^}]*list-style: none;/] }],
    },
    {
      name: 'stackAt md collapses fixed grids at 760px, after the column classes',
      checks: [
        {
          file: 'styles/elements/layout.css',
          has: [/@media \(max-width: 760px\) \{\s*\.bb-grid--stack-md \{ --grid-cols: 1; \}/],
          after: [['.bb-grid--stack-md {', '.bb-grid--6 {']],
        },
      ],
    },
    ...BRAND_TOKEN_FILES.map((file) => ({
      name: `${file} reads brand tokens without literal fallbacks`,
      checks: [
        {
          file: `styles/elements/${file}`,
          lacks: [/var\(--bb-[\w-]+\s*,/, /rgba\(\s*201\s*,\s*168\s*,\s*124/],
        },
      ],
    })),
    {
      name: 'their own display rules yield to [hidden]',
      strip: true,
      checks: [
        { file: 'styles/elements/card.css', has: ['.bb-card[hidden] {\n    display: none;'] },
        { file: 'styles/elements/fact-list.css', has: [':is(.bb-facts, .bb-fact)[hidden] {\n    display: none;'] },
      ],
    },
    {
      name: 'duration and gradient start are hooks with the old defaults',
      strip: true,
      checks: [
        {
          file: 'styles/elements/progress-bar.css',
          has: [
            'transition: transform var(--progress-duration, var(--bb-dur-base)) var(--bb-ease-out-expo);',
            'var(--progress-from, color-mix(in srgb, var(--progress-fill) 50%, transparent))',
            '.bb-progress--success.bb-progress--ramp-2 {\n    --progress-fill: var(--bb-green-light);',
            '.bb-progress--success.bb-progress--ramp-3 {\n    --progress-fill: var(--bb-green);',
          ],
        },
      ],
    },
    {
      name: 'section, hero and topbar tokens keep the values they replace',
      strip: true,
      checks: [
        {
          file: 'styles/brand.css',
          has: [
            '--bb-space-section: clamp(72px, 10vw, 120px);',
            '--bb-space-section-sm: clamp(56px, 8vw, 88px);',
            '--bb-content-hero: 720px;',
            '--bb-topbar-height: calc(58px + env(safe-area-inset-top, 0px));',
            '--bb-topbar-height: calc(52px + env(safe-area-inset-top, 0px));',
          ],
        },
        {
          file: 'styles/elements/layout.css',
          has: [
            '.bb-section--page { --section-pad: var(--bb-space-section); }',
            '.bb-section--page { --section-pad: var(--bb-space-section-sm); }',
          ],
        },
        {
          file: 'styles/elements/page-hero.css',
          has: ['--page-hero-measure: var(--bb-content-hero);'],
          lacks: ['--content-narrow'],
        },
      ],
    },
    {
      name: 'only a moving tile lifts',
      strip: true,
      checks: [{ file: 'styles/elements/stat-tile.css', has: ['.bb-stat:not(.bb-stat--static, .bb-stat--inline):hover {'] }],
    },
    {
      name: 'defaults render the old entrance',
      strip: true,
      checks: [
        {
          file: 'styles/reveal.css',
          has: [
            'var(--stagger-duration, 420ms)',
            'calc(var(--stagger-delay, 30ms) + var(--stagger-i) * var(--stagger-step, 60ms))',
            'var(--stagger-max, 260ms)',
            'translateX(calc(var(--bb-reveal-side, -1) * var(--bb-reveal-shift, 14px)))',
            '.bb-stagger > :nth-child(n + 8) { --stagger-i: 7; }',
          ],
        },
      ],
    },
    {
      name: 'accent cell and roomy padding are styled',
      checks: [
        {
          file: 'styles/elements/table.css',
          has: ['.bb-tbl .acc { color: var(--bb-tan-light); }', /\.bb-tbl--roomy :is\(td, th\[scope="row"\]\) \{[^}]*padding: 12px 16px;/],
        },
      ],
    },
    {
      name: 'the table floor reads its hook and stays auto without it',
      checks: [{ file: 'styles/elements/table.css', has: [/\.bb-tbl \{[^}]*min-width: var\(--tbl-min-w, auto\);/] }],
    },
  ]);
});
