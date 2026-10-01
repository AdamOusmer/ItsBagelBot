// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { astroHtml, contracts, sourceContracts, svelteHtml } from './contract';
import SvelteButton from '../svelte/Button.svelte';
import AstroButton from '../astro/Button.astro';
import SvelteButtonLink from '../svelte/ButtonLink.svelte';
import AstroButtonLink from '../astro/ButtonLink.astro';
import SvelteIconButton from '../svelte/IconButton.svelte';
import AstroIconButton from '../astro/IconButton.astro';
import SvelteInput from '../svelte/Input.svelte';
import AstroInput from '../astro/Input.astro';
import SvelteLabel from '../svelte/Label.svelte';
import AstroLabel from '../astro/Label.astro';
import SvelteChip from '../svelte/Chip.svelte';
import AstroChip from '../astro/Chip.astro';
import SvelteBadge from '../svelte/Badge.svelte';
import AstroBadge from '../astro/Badge.astro';
import SvelteTag from '../svelte/Tag.svelte';
import AstroTag from '../astro/Tag.astro';
import SvelteStatusDot from '../svelte/StatusDot.svelte';
import AstroStatusDot from '../astro/StatusDot.astro';
import SvelteRadioGroup from '../svelte/RadioGroup.svelte';
import AstroRadioGroup from '../astro/RadioGroup.astro';
import SvelteSegmentedControl from '../svelte/SegmentedControl.svelte';
import AstroSegmentedControl from '../astro/SegmentedControl.astro';
import SvelteSwitchRow from '../svelte/SwitchRow.svelte';
import AstroSwitchRow from '../astro/SwitchRow.astro';
import SvelteSlider from '../svelte/Slider.svelte';
import AstroSlider from '../astro/Slider.astro';
import SvelteFileDrop from '../svelte/FileDrop.svelte';
import AstroFileDrop from '../astro/FileDrop.astro';
import SveltePickerOption from '../svelte/PickerOption.svelte';
import AstroPickerOption from '../astro/PickerOption.astro';

const icon12 = (path: string) =>
  '<svg class="bb-icon" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" ' +
  `stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${path}"></svg>`;

const CHECK_ICON = icon12('M20 6L9 17l-5-5');
const X_ICON = icon12('M18 6L6 18M6 6l12 12');

const RELEASES = [{ value: 'v2', label: 'v2.0.0', description: 'abc1234', meta: '2h' }];
const KINDS = [
  { value: 'deploy', label: 'Deploy', description: 'Ship main', meta: '5 stages' },
  { value: 'rollback', label: 'Rollback', disabled: true },
];

const MARK = '<i class="bb-btn__mark" aria-hidden="true"></i>';

describe('Button, ButtonLink and IconButton', () => {
  contracts(SvelteButton, AstroButton, [
    {
      name: 'add is the dashed pill with no diamond mark',
      props: { variant: 'add' },
      slots: { default: '+ Add line' },
      html: '<button class="bb-btn bb-btn--add" type="button" data-mark><span class="bb-btn__content">+ Add line</span></button>',
    },
    {
      name: 'a static button is a span with no button semantics',
      props: { as: 'span', variant: 'secondary', 'data-cursor': '' },
      slots: { default: 'Open guide' },
      html: `<span class="bb-btn bb-btn--secondary bb-btn--static" data-mark data-cursor>${MARK}<span class="bb-btn__content">Open guide</span></span>`,
    },
  ]);

  contracts(SvelteButtonLink, AstroButtonLink, [
    {
      name: 'a disabled link drops its href and says so',
      props: { href: '/p?page=0', variant: 'ghost', disabled: true },
      slots: { default: 'Prev' },
      html: `<a class="bb-btn bb-btn--ghost" role="link" aria-disabled="true" data-mark>${MARK}<span class="bb-btn__content">Prev</span></a>`,
    },
    {
      name: 'brand variant keeps the mark and reads its colours from hooks',
      props: { href: '/auth/login', variant: 'brand', block: true },
      slots: { default: 'Continue' },
      html: `<a class="bb-btn bb-btn--brand bb-btn--block" href="/auth/login" data-mark>${MARK}<span class="bb-btn__content">Continue</span></a>`,
    },
  ]);

  contracts(SvelteIconButton, AstroIconButton, [
    {
      name: 'a danger icon button carries the red hover',
      props: { label: 'Delete deaths', size: 'sm', tone: 'danger' },
      slots: { default: '<svg></svg>' },
      html:
        '<button class="bb-btn bb-btn--icon bb-btn--sm bb-btn--danger-hover" type="button" aria-label="Delete deaths" data-mark>' +
        '<span class="bb-btn__content"><svg></svg></span></button>',
    },
  ]);
});

describe('Input and Label', () => {
  const typed = (type: string, value: string | number) => ({
    name: `${type} keeps its native value and form attributes`,
    props: { type, value, name: 'value', required: true },
    html: `<span class="bb-input"><input type="${type}" value="${value}" name="value" required></span>`,
  });

  contracts(SvelteInput, AstroInput, [
    {
      name: 'color gets the swatch modifier',
      props: { type: 'color', value: '#52b788', name: 'color' },
      html: '<span class="bb-input bb-input--color"><input type="color" value="#52b788" name="color"></span>',
    },
    {
      name: 'read-only is a native readonly input, styled by the contract',
      props: { value: 'Everyone', readonly: true, mono: true, fill: true },
      html: '<span class="bb-input bb-input--fill bb-input--mono"><input type="text" value="Everyone" readonly></span>',
    },
    {
      name: 'Input aligns numbers to the end',
      props: { align: 'end', value: '12', inputmode: 'numeric' },
      html: '<span class="bb-input bb-input--end"><input type="text" value="12" inputmode="numeric"></span>',
    },
    typed('number', 12),
    typed('datetime-local', '2026-09-14T10:30'),
    typed('date', '2026-09-14'),
  ]);

  contracts(SvelteLabel, AstroLabel, [
    {
      name: 'mono label',
      props: { mono: true, htmlFor: 'runs' },
      slots: { default: 'Runs' },
      html: '<label class="bb-label bb-label--mono" for="runs">Runs</label>',
    },
    {
      name: 'a legend label never carries for',
      props: { mono: true, as: 'legend', htmlFor: 'x' },
      slots: { default: 'Sources' },
      html: '<legend class="bb-label bb-label--mono">Sources</legend>',
    },
  ]);
});

describe('Chip, Badge, Tag and StatusDot', () => {
  contracts(SvelteChip, AstroChip, [
    {
      name: 'a static chip is a span with no button semantics',
      props: { as: 'span', tone: 'muted' },
      slots: { default: '$(user)' },
      html: '<span class="bb-chip bb-chip--muted">$(user)</span>',
    },
    {
      name: 'tier chip',
      props: { tone: 'vip', pressed: true, class: 'tier' },
      slots: { default: 'vip' },
      html: '<button type="button" class="bb-chip bb-chip--vip tier" aria-pressed="true" data-pressed>vip</button>',
    },
    {
      name: 'unpressed toggle chip',
      props: { pressed: false },
      slots: { default: 'all' },
      html: '<button type="button" class="bb-chip" aria-pressed="false">all</button>',
    },
    {
      name: 'action chip carries no pressed state',
      slots: { default: 'go' },
      html: '<button type="button" class="bb-chip">go</button>',
    },
    {
      name: 'span chip never claims aria-pressed',
      props: { as: 'span', pressed: true },
      slots: { default: 'beta' },
      html: '<span class="bb-chip" data-pressed>beta</span>',
    },
  ]);

  contracts(SvelteBadge, AstroBadge, [
    {
      name: 'tier badge',
      props: { tone: 'paid', dashed: true },
      slots: { default: 'Sub' },
      html: '<span class="bb-tag bb-badge bb-tag--paid bb-badge--dashed">Sub</span>',
    },
    {
      name: 'dashed literal pill',
      props: { shape: 'pill', tone: 'neutral', dashed: true, literal: true },
      slots: { default: 'No ads, ever.' },
      html: '<span class="bb-tag bb-badge bb-badge--pill bb-tag--neutral bb-badge--dashed bb-tag--literal">No ads, ever.</span>',
    },
    {
      name: 'pill badges preserve non-interactive label semantics',
      props: { shape: 'pill', class: 'tier' },
      slots: { default: 'Paid' },
      html: '<span class="bb-tag bb-badge bb-badge--pill tier">Paid</span>',
    },
  ]);

  contracts(SvelteTag, AstroTag, [
    {
      name: 'status tag',
      props: { tone: 'warning' },
      slots: { default: 'Drawn' },
      html: '<span class="bb-tag bb-tag--warning">Drawn</span>',
    },
    {
      name: 'info tone',
      props: { tone: 'info' },
      slots: { default: 'Event' },
      html: '<span class="bb-tag bb-tag--info">Event</span>',
    },
    {
      name: 'bare together with a tone',
      props: { tone: 'live', bare: true, class: 'chat-tag' },
      slots: { default: 'Rehearsal' },
      html: '<span class="bb-tag bb-tag--live bb-tag--bare chat-tag">Rehearsal</span>',
    },
    {
      name: 'literal command chip',
      props: { tone: 'bare', literal: true },
      slots: { default: '!so' },
      html: '<span class="bb-tag bb-tag--bare bb-tag--literal">!so</span>',
    },
  ]);

  const dot = (tone: string, flat = false) => ({
    name: `status dot ${tone}${flat ? ' flat' : ''} is decorative`,
    props: { tone, flat },
    html: `<span class="bb-status-dot ${tone}${flat ? ' flat' : ''}" aria-hidden="true"></span>`,
  });

  contracts(SvelteStatusDot, AstroStatusDot, [
    dot('success'),
    dot('warning'),
    dot('danger'),
    dot('neutral'),
    dot('warning', true),
  ]);
});

describe('RadioGroup', () => {
  contracts(SvelteRadioGroup, AstroRadioGroup, [
    {
      name: 'cards carry a tick, description and meta; disabled options stay in the group',
      props: { name: 'k', value: 'deploy', variant: 'cards', min: '150px', options: KINDS },
      html:
        '<div class="bb-choices bb-choices--cards" role="radiogroup" aria-label="Options" style="--choices-min: 150px;">' +
        '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="k" value="deploy" checked>' +
        `<span class="bb-choice__top"><span class="bb-choice__tick" aria-hidden="true">${CHECK_ICON}</span></span>` +
        '<span class="bb-choice__label">Deploy</span><span class="bb-choice__desc">Ship main</span>' +
        '<span class="bb-choice__meta">5 stages</span></label>' +
        '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="k" value="rollback" disabled>' +
        `<span class="bb-choice__top"><span class="bb-choice__tick" aria-hidden="true">${CHECK_ICON}</span></span>` +
        '<span class="bb-choice__label">Rollback</span></label></div>',
    },
    {
      name: 'rows keep the native radio',
      props: { name: 'r', value: 'v2', variant: 'rows', label: 'Rollback to', options: RELEASES },
      html:
        '<div class="bb-choices bb-choices--rows" role="radiogroup" aria-label="Rollback to">' +
        '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="r" value="v2" checked>' +
        '<span class="bb-choice__label">v2.0.0</span><span class="bb-choice__desc">abc1234</span>' +
        '<span class="bb-choice__meta">2h</span></label></div>',
    },
    {
      name: 'tabs accept rich options and ignore their description',
      props: {
        name: 't',
        value: 'a',
        options: [{ value: 'a', label: 'A', description: 'x' }, { value: 'b', label: 'B', disabled: true }],
      },
      html:
        '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Options">' +
        '<label class="bb-tab is-active"><input class="bb-tab__input" type="radio" name="t" value="a" checked> A</label>' +
        '<label class="bb-tab "><input class="bb-tab__input" type="radio" name="t" value="b" disabled> B</label></div>',
    },
    {
      name: 'cards take a fixed column count and a rail',
      props: { name: 'k', value: 'deploy', variant: 'cards', min: '168px', cols: 5, rail: 'md', options: [KINDS[0]] },
      html:
        '<div class="bb-choices bb-choices--cards bb-choices--cols bb-choices--rail-md" role="radiogroup" aria-label="Options" ' +
        'style="--choices-min: 168px; --choices-cols: 5;">' +
        '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="k" value="deploy" checked>' +
        `<span class="bb-choice__top"><span class="bb-choice__tick" aria-hidden="true">${CHECK_ICON}</span></span>` +
        '<span class="bb-choice__label">Deploy</span><span class="bb-choice__desc">Ship main</span>' +
        '<span class="bb-choice__meta">5 stages</span></label></div>',
    },
    {
      name: 'rows scroll inside their own border',
      props: { name: 'r', value: 'v2', variant: 'rows', maxHeight: '360px', options: [{ value: 'v2', label: 'v2.0.0' }] },
      html:
        '<div class="bb-choices bb-choices--rows bb-choices--scroll bb-scroll" role="radiogroup" aria-label="Options" ' +
        'style="--choices-max-h: 360px;"><label class="bb-choice"><input class="bb-choice__input" type="radio" name="r" ' +
        'value="v2" checked><span class="bb-choice__label">v2.0.0</span></label></div>',
    },
  ]);

  test('svelte cards render the leading snippet with the selected flag', () => {
    const leading = createRawSnippet((opt: () => { label: string }, on: () => boolean) => ({
      render: () => `<b>${opt().label[0]}${on() ? '*' : ''}</b>`,
    }));
    const html = svelteHtml(SvelteRadioGroup, { name: 'k', value: 'deploy', variant: 'cards', options: KINDS, leading });
    expect(html).toContain('<span class="bb-choice__lead"><b>D*</b></span>');
    expect(html).toContain('<span class="bb-choice__lead"><b>R</b></span>');
  });
});

describe('SegmentedControl', () => {
  contracts(SvelteSegmentedControl, AstroSegmentedControl, [
    {
      name: 'option objects carry counts beside plain strings',
      props: {
        value: 'all',
        label: 'Source',
        options: [{ value: 'all', label: 'All', count: 12 }, { value: 'mod', label: 'Modules', count: 0 }, 'Plain'],
      },
      html:
        '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Source">' +
        '<button type="button" class="bb-tab is-active" role="radio" aria-checked="true" tabindex="0" value="all">All<span class="bb-tab__count">12</span></button>' +
        '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="mod">Modules<span class="bb-tab__count">0</span></button>' +
        '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="Plain">Plain</button></div>',
    },
    {
      name: 'segment buttons carry their value',
      props: { value: 'custom', label: 'Mode', options: [{ value: 'custom', label: 'Custom' }, 'plain'] },
      html:
        '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Mode">' +
        '<button type="button" class="bb-tab is-active" role="radio" aria-checked="true" tabindex="0" value="custom">Custom</button>' +
        '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="plain">plain</button></div>',
    },
    {
      name: 'segment options pass their own attributes to the button',
      props: {
        value: 'custom',
        label: 'Mode',
        options: [{ value: 'custom', label: 'Custom', attrs: { 'data-mode': 'custom' } }, 'plain'],
      },
      html:
        '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Mode">' +
        '<button type="button" class="bb-tab is-active" role="radio" aria-checked="true" tabindex="0" value="custom" data-mode="custom">Custom</button>' +
        '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="plain">plain</button></div>',
    },
  ]);
});

describe('SwitchRow', () => {
  contracts(SvelteSwitchRow, AstroSwitchRow, [
    {
      name: 'switch first, hint wired to the switch',
      props: { label: 'Enabled', hint: 'Runs in chat', hintId: 'h1', checked: true },
      html:
        '<div class="bb-switch-row"><button type="button" class="bb-switch" role="switch" aria-checked="true" ' +
        'aria-label="Enabled" aria-describedby="h1" data-state="on"></button><span class="bb-switch-row__text">' +
        '<span class="bb-switch-row__label">Enabled</span><span class="bb-switch-row__hint" id="h1">Runs in chat</span></span></div>',
    },
    {
      name: 'switch last, own accessible name, submit type',
      props: { label: 'Streamer points', hint: 'Earn too', hintId: 'h2', control: 'end', switchLabel: 'Points', type: 'submit' },
      html:
        '<div class="bb-switch-row bb-switch-row--end"><span class="bb-switch-row__text">' +
        '<span class="bb-switch-row__label">Streamer points</span><span class="bb-switch-row__hint" id="h2">Earn too</span></span>' +
        '<button type="submit" class="bb-switch" role="switch" aria-checked="false" aria-label="Points" ' +
        'aria-describedby="h2" data-state="off"></button></div>',
    },
    {
      name: 'status sits between the text and the switch, warn tones the label',
      props: { label: 'Live only', control: 'end', tone: 'warning' },
      slots: { status: '<em>Saved</em>' },
      html:
        '<div class="bb-switch-row bb-switch-row--end bb-switch-row--warning"><span class="bb-switch-row__text">' +
        '<span class="bb-switch-row__label">Live only</span></span><span class="bb-switch-row__status"><em>Saved</em></span>' +
        '<button type="button" class="bb-switch" role="switch" aria-checked="false" aria-label="Live only" data-state="off"></button></div>',
    },
  ]);

  test('the note renders on its own line after the switch in both adapters', async () => {
    const note = '<small>Refused</small>';
    const tail = 'data-state="off"></button><span class="bb-switch-row__note"><small>Refused</small></span></div>';
    expect(svelteHtml(SvelteSwitchRow, { label: 'On', control: 'end' }, { note }).endsWith(tail)).toBe(true);
    expect((await astroHtml(AstroSwitchRow, { label: 'On', control: 'end' }, { note })).endsWith(tail)).toBe(true);
  });
});

describe('Slider and FileDrop', () => {
  contracts(SvelteSlider, AstroSlider, [
    {
      name: 'Slider is a range input',
      props: { value: 12, min: 1, max: 60, name: 'runs' },
      html: '<input type="range" class="bb-slider" min="1" max="60" step="1" value="12" name="runs">',
    },
  ]);

  contracts(SvelteFileDrop, AstroFileDrop, [
    {
      name: 'FileDrop labels its hidden input with the hint',
      props: { label: 'Drop a CSV', accept: '.csv', name: 'file' },
      html:
        '<label class="bb-file-drop"><input type="file" class="bb-file-drop__input" accept=".csv" name="file">' +
        '<span class="bb-file-drop__text">Drop a CSV</span></label>',
    },
  ]);
});

describe('PickerOption', () => {
  contracts(SveltePickerOption, AstroPickerOption, [
    {
      name: 'stacked row in a list, armed remove shows its word',
      props: {
        label: '$(fetch.weather)',
        description: 'data.temp',
        layout: 'stacked',
        as: 'li',
        remove: { label: 'Delete weather', armed: true, armedLabel: 'Delete' },
      },
      html:
        '<li class="bb-picker-option bb-picker-option--stacked"><button type="button" class="bb-picker-option__main">' +
        '<span class="bb-picker-option__label">$(fetch.weather)</span><span class="bb-picker-option__desc">data.temp</span></button>' +
        '<button type="button" class="bb-picker-option__remove" aria-label="Delete weather" data-armed>Delete</button></li>',
    },
    {
      name: 'selected and disabled, attributes land on the row button',
      props: { label: 'deaths', description: 'channel', disabled: true, selected: true, title: 'tip', remove: { label: 'Delete' } },
      html:
        '<div class="bb-picker-option"><button type="button" class="bb-picker-option__main" aria-current="true" disabled title="tip">' +
        '<span class="bb-picker-option__label">deaths</span><span class="bb-picker-option__desc">channel</span></button>' +
        `<button type="button" class="bb-picker-option__remove" aria-label="Delete">${X_ICON}</button></div>`,
    },
    {
      name: 'a custom body replaces the label in both adapters',
      props: { label: 'ignored' },
      slots: { default: '<code>$(user)</code>' },
      html:
        '<div class="bb-picker-option"><button type="button" class="bb-picker-option__main"><code>$(user)</code></button></div>',
    },
  ]);
});

const FALLBACKLESS = ['elements/field.css', 'elements/typography.css', 'elements/button.css', 'elements/tag.css', 'elements/chip.css'];
const rail = (size: string, width: number) =>
  new RegExp(
    `@media \\(max-width: ${width}px\\) \\{\\s*\\.bb-choices--rail-${size} \\{[^}]*grid-template-columns: none;` +
      '[^}]*grid-auto-flow: column;[^}]*grid-auto-columns: minmax\\(var\\(--choices-min, 200px\\), 1fr\\);' +
      '[^}]*overflow-x: auto;[^}]*scroll-snap-type: x mandatory;',
  );

describe('control stylesheets', () => {
  sourceContracts([
    {
      name: 'columns, rails and the card height hook are styled, rails after columns',
      checks: [
        {
          file: 'styles/elements/radio-group.css',
          has: [
            'grid-template-columns: repeat(var(--choices-cols), minmax(0, 1fr));',
            rail('md', 980),
            rail('sm', 560),
            '> .bb-choice { scroll-snap-align: start; }',
            /\.bb-choices--cards \.bb-choice \{[^}]*min-height: var\(--choice-min-h, auto\);/,
          ],
          after: [
            ['.bb-choices--rail-md {', '.bb-choices--cols {'],
            ['.bb-choices--rail-sm {', '.bb-choices--cols {'],
          ],
        },
      ],
    },
    {
      name: 'the astro tabs sync is-active with the checked radio',
      checks: [
        {
          file: 'astro/RadioGroup.astro',
          has: ["document.addEventListener('change', syncTabs);", "classList.toggle('is-active', radio.checked)"],
        },
      ],
    },
    {
      name: 'error red comes from the status token, not a literal',
      strip: true,
      checks: [{ file: FALLBACKLESS.map((file) => `styles/${file}`), lacks: [/#cf8a78/i] }],
    },
    {
      name: 'tier tones read the tier tokens, and vip stays silver',
      strip: true,
      checks: [
        {
          file: ['styles/elements/tag.css', 'styles/elements/chip.css'],
          has: ['free', 'paid', 'vip', 'banned', 'inactive'].map((tier) => `--tone: var(--bb-tier-${tier});`),
          lacks: [/#d9aaff|purple/i],
        },
      ],
    },
    {
      name: 'a chip shows its tier colour only while pressed',
      strip: true,
      checks: [
        {
          file: 'styles/elements/chip.css',
          has: ['.bb-chip[data-pressed]:is(.bb-chip--free, .bb-chip--paid, .bb-chip--vip, .bb-chip--banned, .bb-chip--inactive)'],
        },
      ],
    },
    {
      name: 'a static button follows the hover of the link around it, and brand reads only hooks',
      strip: true,
      checks: [
        { file: 'styles/elements/button.css', has: ['a:hover .bb-btn--static.bb-btn--primary {', 'a:hover .bb-btn--static.bb-btn--secondary {'] },
        {
          file: 'styles/elements/button.css',
          between: ['.bb-btn--brand {', '.bb-btn--add {'],
          has: ['background: var(--btn-brand);', 'box-shadow: 0 0 24px var(--btn-brand-glow, transparent);'],
          lacks: [/#[0-9a-f]{3,6}\b|rgba\(\d/i],
        },
      ],
    },
    {
      name: 'warn reads the warn token',
      strip: true,
      checks: [
        {
          file: 'styles/elements/toggle.css',
          has: ['.bb-switch-row--warning .bb-switch-row__label {\n        color: var(--bb-warn);'],
        },
      ],
    },
    {
      name: 'info joins the toned group',
      checks: [{ file: 'styles/elements/tag.css', has: ['.bb-tag--info', /\.bb-tag:is\([^)]*\.bb-tag--info\)/] }],
    },
    {
      name: 'dashed pill dashes every side',
      checks: [{ file: 'styles/elements/badge.css', has: ['.bb-badge--pill.bb-badge--dashed { border-style: dashed; }'] }],
    },
  ]);
});
