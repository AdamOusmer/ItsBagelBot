// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet, type Component } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { findUnnamed } from '../scripts/control-names';
import { normalise, removeAll } from './normalise';
import SvelteCardHead from '../svelte/CardHead.svelte';
import AstroCardHead from '../astro/CardHead.astro';
import SvelteCheckbox from '../svelte/Checkbox.svelte';
import AstroCheckbox from '../astro/Checkbox.astro';
import SvelteCommunityCard from '../svelte/CommunityCard.svelte';
import AstroCommunityCard from '../astro/CommunityCard.astro';
import SvelteInput from '../svelte/Input.svelte';
import AstroInput from '../astro/Input.astro';
import SvelteRankingCard from '../svelte/RankingCard.svelte';
import AstroRankingCard from '../astro/RankingCard.astro';
import SvelteSectionHeading from '../svelte/SectionHeading.svelte';
import AstroSectionHeading from '../astro/SectionHeading.astro';
import SvelteSegmentedControl from '../svelte/SegmentedControl.svelte';
import AstroSegmentedControl from '../astro/SegmentedControl.astro';
import SvelteSelect from '../svelte/Select.svelte';
import AstroSelect from '../astro/Select.astro';
import SvelteSlider from '../svelte/Slider.svelte';
import AstroSlider from '../astro/Slider.astro';
import SvelteStepList from '../svelte/StepList.svelte';
import SvelteTextarea from '../svelte/Textarea.svelte';
import AstroTextarea from '../astro/Textarea.astro';
import SvelteFieldControl from './fixtures/field-control.svelte';
import AstroFieldControl from './fixtures/field-control.astro';

const container = await experimental_AstroContainer.create();
const svelte = (component: unknown, props: Record<string, unknown>) =>
  normalise(render(component as Component<Record<string, unknown>>, { props }).body);
const astro = async (component: Parameters<typeof container.renderToString>[0], props: Record<string, unknown>) =>
  normalise(await container.renderToString(component, { props }));

const headingTag = (html: string, className: string) =>
  new RegExp(`<(h[1-6])[^>]*class="${className}"`).exec(html)?.[1];

const ITEMS = [{ id: 'a', label: 'Alpha', value: 3, valueLabel: '3' }];

const HEADED = [
  { name: 'CardHead', svelte: SvelteCardHead, astro: AstroCardHead, props: { title: 'Recent' }, className: 'bb-card-head__title', fallback: 'h3' },
  { name: 'SectionHeading', svelte: SvelteSectionHeading, astro: AstroSectionHeading, props: { title: 'Games' }, className: 'bb-section-heading__title', fallback: 'h2' },
  { name: 'CommunityCard', svelte: SvelteCommunityCard, astro: AstroCommunityCard, props: { title: 'Guild', total: '12' }, className: 'bb-community-card__title', fallback: 'h2' },
  { name: 'RankingCard', svelte: SvelteRankingCard, astro: AstroRankingCard, props: { title: 'Top', items: ITEMS }, className: 'bb-ranking-card__title', fallback: 'h2' },
];

describe('headingLevel', () => {
  for (const block of HEADED) {
    test(`${block.name}: keeps its historical level by default and follows headingLevel in both adapters`, async () => {
      const byDefault = svelte(block.svelte, block.props);
      expect(headingTag(byDefault, block.className)).toBe(block.fallback);
      expect(await astro(block.astro, block.props)).toBe(byDefault);

      const props = { ...block.props, headingLevel: 5 };
      const leveled = svelte(block.svelte, props);
      expect(headingTag(leveled, block.className)).toBe('h5');
      expect(leveled).toContain('</h5>');
      expect(await astro(block.astro, props)).toBe(leveled);
      expect(leveled.replace(/h5/g, block.fallback)).toBe(byDefault);
    });
  }
});

describe('StepList navigation', () => {
  const steps = [
    { id: 'build', label: 'Build', state: 'succeeded' },
    { id: 'roll', label: 'Rollout', state: 'running' },
  ];
  const html = svelte(SvelteStepList, { steps, selected: 'roll', label: 'Stages', onSelect: () => {} });

  test('every selectable row speaks its state as text', () => {
    expect(html).toContain('<span class="bb-step__label">Build</span><span class="bb-sr-only">Succeeded</span>');
    expect(html).toContain('<span class="bb-step__label">Rollout</span><span class="bb-sr-only">Running</span>');
  });

  test('caller state words replace the catalog words', () => {
    const french = svelte(SvelteStepList, { steps, label: 'Étapes', stateLabels: { running: 'En cours' }, onSelect: () => {} });
    expect(french).toContain('<span class="bb-sr-only">En cours</span>');
  });
});

describe('SegmentedControl', () => {
  const options = ['all', 'live', 'off'];
  const stops = (html: string) => [...html.matchAll(/tabindex="(-?\d)"/g)].map((match) => match[1]);

  test('the checked radio is the only tab stop', async () => {
    const props = { options, value: 'live', label: 'Show' };
    const html = svelte(SvelteSegmentedControl, props);
    expect(stops(html)).toEqual(['-1', '0', '-1']);
    expect(await astro(AstroSegmentedControl, props)).toBe(html);
  });

  test('with nothing checked the first radio is the tab stop', async () => {
    const props = { options, value: 'elsewhere', label: 'Show' };
    const html = svelte(SvelteSegmentedControl, props);
    expect(stops(html)).toEqual(['0', '-1', '-1']);
    expect(await astro(AstroSegmentedControl, props)).toBe(html);
  });

  test('the label defaults to the catalog word', () => {
    expect(svelte(SvelteSegmentedControl, { options, value: 'all' })).toContain('aria-label="Filter"');
  });

  test('a caller keydown runs after the arrow-key handler instead of replacing it', async () => {
    const source = await Bun.file(new URL('../svelte/SegmentedControl.svelte', import.meta.url)).text();
    expect(source).toContain('onkeydown: callerKeydown');
    expect(source).toContain('{...rest} {onkeydown}');
    expect(source.indexOf('next.click()')).toBeLessThan(source.indexOf('callerKeydown?.(event)'));
  });
});

const CONTROLS = [
  { name: 'Checkbox', svelte: SvelteCheckbox, astro: AstroCheckbox, props: {} },
  { name: 'Input', svelte: SvelteInput, astro: AstroInput, props: {} },
  { name: 'Select', svelte: SvelteSelect, astro: AstroSelect, props: { options: [{ value: 'a', label: 'A' }] } },
  { name: 'Slider', svelte: SvelteSlider, astro: AstroSlider, props: {} },
  { name: 'Textarea', svelte: SvelteTextarea, astro: AstroTextarea, props: {} },
];

const FORM_CONTROL = /<(input|textarea|select)\b[^>]*>/;

function isNamed(html: string): boolean {
  const control = FORM_CONTROL.exec(html)?.[0] ?? '';
  if (/aria-label="[^"]+"|aria-labelledby="[^"]+"/.test(control)) return true;
  const label = /<label\b[^>]*>([\s\S]*?)<\/label>/.exec(html)?.[1] ?? '';
  return removeAll(removeAll(label, /<select[\s\S]*?<\/select>/g), /<[^>]*>/g).trim() !== '';
}

describe('form controls always have an accessible name', () => {
  for (const control of CONTROLS) {
    test(`${control.name}: bare is unnamed, a Field or aria-label names it in both adapters`, async () => {
      expect(isNamed(svelte(control.svelte, control.props))).toBe(false);
      expect(isNamed(await astro(control.astro, control.props))).toBe(false);

      expect(isNamed(svelte(SvelteFieldControl, { kind: control.name }))).toBe(true);
      expect(isNamed(await astro(AstroFieldControl, { kind: control.name }))).toBe(true);

      const labelled = { ...control.props, 'aria-label': 'Channel' };
      expect(isNamed(svelte(control.svelte, labelled))).toBe(true);
      expect(isNamed(await astro(control.astro, labelled))).toBe(true);
    });
  }

  test('Checkbox children are its name', () => {
    const children = createRawSnippet(() => ({ render: () => 'Remember me' }));
    expect(isNamed(svelte(SvelteCheckbox, { children }))).toBe(true);
  });

  test('Select label names the trigger button', () => {
    expect(svelte(SvelteSelect, { options: [], label: 'Region' })).toContain('aria-label="Region"');
  });
});

describe('call-site scan', () => {
  const pkg = ['@bagel', 'ui'].join('/');
  const imports = ['Input', 'Checkbox', 'Field'].map((name) => `import ${name} from '${pkg}/svelte/${name}.svelte';\n`).join('');

  test('flags a bare control and accepts each way of naming one', () => {
    const cases: [string, number][] = [
      ['<Input />', 1],
      ['<Input aria-label="Name" />', 0],
      ['<Input\n  label={x}\n/>', 0],
      ['<Field label="Name"><Input /></Field>', 0],
      ['<Field label="Name"></Field><Input />', 1],
      ['<label for="n">Name</label><Input id="n" />', 0],
      ['<Input id="n" />', 1],
      ['<Checkbox>Remember</Checkbox>', 0],
      ['<Checkbox />', 1],
      ['<Input onkeydown={(e) => e.key === \'>\' && go()} />', 1],
    ];
    for (const [markup, expected] of cases) {
      expect([markup, findUnnamed(imports + markup).length]).toEqual([markup, expected]);
    }
  });

  test('ignores components that are not the shared controls', () => {
    expect(findUnnamed("import Input from './Input.svelte';\n<Input />")).toEqual([]);
  });
});
