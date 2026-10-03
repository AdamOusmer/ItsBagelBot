// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { findUnnamed } from '../scripts/control-names';
import { astroHtml, snippet, sourceContracts, svelteHtml } from './contract';
import { removeAll } from './normalise';
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
import SvelteModal from '../svelte/Modal.svelte';
import AstroModal from '../astro/Modal.astro';
import ConfirmDialog from '../svelte/ConfirmDialog.svelte';
import SvelteTooltip from '../svelte/Tooltip.svelte';
import AstroTooltip from '../astro/Tooltip.astro';
import Popover from '../svelte/Popover.svelte';
import PickerPanel from '../svelte/PickerPanel.svelte';
import ProfileMenu from '../svelte/ProfileMenu.svelte';
import SvelteCopySurface from '../svelte/CopySurface.svelte';
import SvelteFieldControl from './fixtures/field-control.svelte';
import AstroFieldControl from './fixtures/field-control.astro';

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
      const byDefault = svelteHtml(block.svelte, block.props);
      expect(headingTag(byDefault, block.className)).toBe(block.fallback);
      expect(await astroHtml(block.astro, block.props)).toBe(byDefault);

      const props = { ...block.props, headingLevel: 5 };
      const leveled = svelteHtml(block.svelte, props);
      expect(headingTag(leveled, block.className)).toBe('h5');
      expect(leveled).toContain('</h5>');
      expect(await astroHtml(block.astro, props)).toBe(leveled);
      expect(leveled.replace(/h5/g, block.fallback)).toBe(byDefault);
    });
  }
});

describe('StepList navigation', () => {
  const steps = [
    { id: 'build', label: 'Build', state: 'succeeded' },
    { id: 'roll', label: 'Rollout', state: 'running' },
  ];

  test('every selectable row speaks its state as text', () => {
    const html = svelteHtml(SvelteStepList, { steps, selected: 'roll', label: 'Stages', onSelect: () => {} });
    expect(html).toContain('<span class="bb-step__label">Build</span><span class="bb-sr-only">Succeeded</span>');
    expect(html).toContain('<span class="bb-step__label">Rollout</span><span class="bb-sr-only">Running</span>');
  });

  test('caller state words replace the catalog words', () => {
    const french = svelteHtml(SvelteStepList, { steps, label: 'Étapes', stateLabels: { running: 'En cours' }, onSelect: () => {} });
    expect(french).toContain('<span class="bb-sr-only">En cours</span>');
  });
});

describe('SegmentedControl', () => {
  const options = ['all', 'live', 'off'];
  const stops = (html: string) => [...html.matchAll(/tabindex="(-?\d)"/g)].map((match) => match[1]);

  test.each([
    ['the checked radio is the only tab stop', 'live', ['-1', '0', '-1']],
    ['with nothing checked the first radio is the tab stop', 'elsewhere', ['0', '-1', '-1']],
  ])('%s', async (_name, value, expected) => {
    const props = { options, value, label: 'Show' };
    const html = svelteHtml(SvelteSegmentedControl, props);
    expect(stops(html)).toEqual(expected);
    expect(await astroHtml(AstroSegmentedControl, props)).toBe(html);
  });

  test('the label defaults to the catalog word', () => {
    expect(svelteHtml(SvelteSegmentedControl, { options, value: 'all' })).toContain('aria-label="Filter"');
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
      expect(isNamed(svelteHtml(control.svelte, control.props))).toBe(false);
      expect(isNamed(await astroHtml(control.astro, control.props))).toBe(false);

      expect(isNamed(svelteHtml(SvelteFieldControl, { kind: control.name }))).toBe(true);
      expect(isNamed(await astroHtml(AstroFieldControl, { kind: control.name }))).toBe(true);

      const labelled = { ...control.props, 'aria-label': 'Channel' };
      expect(isNamed(svelteHtml(control.svelte, labelled))).toBe(true);
      expect(isNamed(await astroHtml(control.astro, labelled))).toBe(true);
    });
  }

  test('Checkbox children are its name', () => {
    expect(isNamed(svelteHtml(SvelteCheckbox, {}, { default: 'Remember me' }))).toBe(true);
  });

  test('Select label names the trigger button', () => {
    expect(svelteHtml(SvelteSelect, { options: [], label: 'Region' })).toContain('aria-label="Region"');
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

describe('Modal names and descriptions', () => {
  test('a titled dialog is labelled by its heading at the requested level', () => {
    const out = svelteHtml(SvelteModal, { open: true, title: 'Delete', headingLevel: 2 });
    expect(out).toMatch(/aria-labelledby="(bb-modal-title-[^"]+)"/);
    expect(out).toMatch(/<h2 class="bb-modal__title" id="bb-modal-title-[^"]+">Delete<\/h2>/);
  });

  test('the heading level defaults to the previous h3', () => {
    expect(svelteHtml(SvelteModal, { open: true, title: 'Delete' })).toContain('<h3 class="bb-modal__title"');
  });

  test('a label-only dialog carries aria-label and no heading', () => {
    const out = svelteHtml(SvelteModal, { open: true, label: 'Diagram' });
    expect(out).toContain('aria-label="Diagram"');
    expect(out).not.toContain('aria-labelledby');
    expect(out).not.toContain('<h3');
  });

  test('describedBy and role reach the dialog card', () => {
    const out = svelteHtml(SvelteModal, { open: true, title: 'Delete', role: 'alertdialog', describedBy: 'why' });
    expect(out).toContain('role="alertdialog"');
    expect(out).toContain('aria-describedby="why"');
  });

  test('the astro twin matches the svelte name, description and level', async () => {
    const out = await astroHtml(AstroModal, { title: 'Delete', headingLevel: 2, role: 'alertdialog', describedBy: 'why' });
    expect(out).toContain('role="alertdialog"');
    expect(out).toContain('aria-describedby="why"');
    expect(out).toContain('<h2 class="bb-modal__title" id="bb-modal-title">Delete</h2>');
  });
});

describe('ConfirmDialog', () => {
  const base = { open: true, title: 'Delete', onConfirm: () => {} };

  test('the body is the dialog description', () => {
    const out = svelteHtml(ConfirmDialog, { ...base, body: 'This cannot be undone.' });
    const id = /aria-describedby="([^"]+)"/.exec(out)?.[1];
    expect(id).toBeTruthy();
    expect(out).toContain(`<p class="bb-modal__body" id="${id}">This cannot be undone.</p>`);
    expect(out).toContain('role="dialog"');
  });

  test('a danger confirmation is an alertdialog', () => {
    expect(svelteHtml(ConfirmDialog, { ...base, body: 'Gone.', tone: 'danger' })).toContain('role="alertdialog"');
  });

  test('no body means nothing to describe', () => {
    expect(svelteHtml(ConfirmDialog, base)).not.toContain('aria-describedby');
  });
});

describe('Tooltip', () => {
  test('the bubble always has an id and role and the astro twin agrees when given one', async () => {
    const out = svelteHtml(SvelteTooltip, { text: 'Copy' }, { default: '<button>x</button>' });
    expect(out).toMatch(/<span class="bb-tooltip__bubble" id="bb-tooltip-[^"]+" role="tooltip">Copy<\/span>/);
    expect(out).not.toContain('aria-hidden');
    expect(await astroHtml(AstroTooltip, { text: 'Copy', id: 'tt' })).toContain('id="tt" role="tooltip"');
  });

  test('an explicit id is kept', () => {
    expect(svelteHtml(SvelteTooltip, { text: 'Copy', id: 'tt', children: snippet('') })).toContain('id="tt" role="tooltip"');
  });
});

describe('Popover', () => {
  const base = { label: 'Install', title: 'Add', pill: snippet('<span>Install</span>') };

  test('the heading level is configurable and defaults to h2', () => {
    expect(svelteHtml(Popover, { ...base, open: true })).toContain('<h2 class="bb-h bb-h--l6"');
    expect(svelteHtml(Popover, { ...base, open: true, headingLevel: 3 })).toContain('<h3 class="bb-h bb-h--l6"');
  });

  test('an empty dismiss label falls back to the catalog name', () => {
    const out = svelteHtml(Popover, { ...base, dismissLabel: '', onDismiss: () => {} });
    expect(out).toContain('class="bb-popover__x" type="button" aria-label="Dismiss"');
  });

  test('the trigger controls the sheet only while open', () => {
    expect(svelteHtml(Popover, base)).not.toContain('aria-controls');
    const open = svelteHtml(Popover, { ...base, open: true });
    const id = /aria-controls="([^"]+)"/.exec(open)?.[1];
    expect(open).toContain(`id="${id}" role="dialog"`);
  });
});

describe('PickerPanel', () => {
  test('the dropdown is a focusable, identified dialog', () => {
    const out = svelteHtml(PickerPanel, { open: true, label: 'Pick', children: snippet('<button>a</button>') });
    expect(out).toMatch(/role="dialog" aria-label="Pick" id="bb-picker-[^"]+" tabindex="-1"/);
  });

  test('a caller can name the panel id it wires to its own anchor', () => {
    expect(svelteHtml(PickerPanel, { open: true, label: 'Pick', id: 'mine', children: snippet('') })).toContain('id="mine"');
  });
});

describe('ProfileMenu', () => {
  const base = { name: 'Mavey', caption: 'Owner', logoutLabel: 'Log out', avatar: snippet('<i></i>') };
  const links = [{ href: '/a', label: 'A' }];
  const help = [{ href: '/h', label: 'Help' }];

  test('the topbar trigger controls a named menu while open', () => {
    const out = svelteHtml(ProfileMenu, { ...base, open: true, links });
    const id = /aria-controls="([^"]+)"/.exec(out)?.[1];
    expect(out).toContain(`id="${id}" role="menu" aria-label="Account menu"`);
    expect(svelteHtml(ProfileMenu, base)).not.toContain('aria-controls');
  });

  test('only menuitems and groups sit inside the menu', () => {
    const out = svelteHtml(ProfileMenu, { ...base, open: true, links, help, helpTitle: 'Support' });
    expect(out).toContain('<div class="bb-profile__head" aria-hidden="true">');
    expect(out).toContain('<div class="bb-profile__section" aria-hidden="true">Support</div>');
    expect(out).toContain('role="group" aria-label="Support"');
    expect(out).toContain('<form method="POST" action="/auth/logout" role="none">');
  });

  test('the help group falls back to the help label when there is no title', () => {
    expect(svelteHtml(ProfileMenu, { ...base, open: true, help })).toContain('role="group" aria-label="Support"');
  });

  test('the rail menus are named and controlled', () => {
    const out = svelteHtml(ProfileMenu, { ...base, variant: 'rail', open: true, links, help });
    expect(out).toMatch(/aria-controls="bb-profile-menu-[^"]+"/);
    expect(out).toContain('role="menu" aria-label="Account menu"');
    expect(svelteHtml(ProfileMenu, { ...base, variant: 'rail', helpOpen: true, help })).toMatch(
      /id="bb-profile-help-[^"]+" role="menu" aria-label="Support"/,
    );
  });
});

describe('CopySurface', () => {
  test('caller attributes cannot replace the copy payload or the type', () => {
    const out = svelteHtml(SvelteCopySurface, { text: 'BAGEL', 'data-copy': 'other', type: 'submit' });
    expect(out).toContain('data-copy="BAGEL"');
    expect(out).toContain('type="button"');
    expect(out).not.toContain('other');
    expect(out).not.toContain('submit');
  });
});

describe('overlay stylesheets, markup and prop types', () => {
  sourceContracts([
    {
      name: 'the dismissed state hides the bubble and beats the hover rule',
      checks: [
        {
          file: 'styles/elements/tooltip.css',
          after: [['.bb-tooltip[data-dismissed] .bb-tooltip__bubble', '.bb-tooltip:is(:hover, :focus-within) .bb-tooltip__bubble']],
        },
      ],
    },
    {
      name: 'a nameless dialog and a toolbar without a label do not type-check',
      checks: [
        {
          file: 'svelte/Modal.svelte',
          has: [
            'type Named = { title: string } | { title?: undefined; label: string };',
            '{ toolbar: Snippet; toolbarLabel: string }',
          ],
        },
      ],
    },
    {
      name: 'the sheet scrim is neither a tab stop nor announced',
      checks: [
        {
          file: 'svelte/PickerPanel.svelte',
          has: ['class="bb-picker-panel__scrim" type="button" tabindex="-1" aria-hidden="true"'],
        },
      ],
    },
  ]);
});
