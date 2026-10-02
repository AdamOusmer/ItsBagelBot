// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { astroHtml, bothHtml, contracts, snippet, sourceContracts, svelteHtml } from './contract';
import { normalise } from './normalise';
import { icons, type IconName } from '../lib/icons';
import SvelteTextLink from '../svelte/TextLink.svelte';
import AstroTextLink from '../astro/TextLink.astro';
import SvelteSectionNav from '../svelte/SectionNav.svelte';
import AstroSectionNav from '../astro/SectionNav.astro';
import SvelteMark from '../svelte/Mark.svelte';
import AstroMark from '../astro/Mark.astro';
import SvelteAlertBanner from '../svelte/AlertBanner.svelte';
import AstroAlertBanner from '../astro/AlertBanner.astro';
import SvelteErrorScene from '../svelte/ErrorScene.svelte';
import AstroErrorScene from '../astro/ErrorScene.astro';
import SvelteModal from '../svelte/Modal.svelte';
import AstroModal from '../astro/Modal.astro';
import SvelteInspectorSurface from '../svelte/InspectorSurface.svelte';
import AstroInspectorSurface from '../astro/InspectorSurface.astro';
import SvelteSkipLink from '../svelte/SkipLink.svelte';
import AstroSkipLink from '../astro/SkipLink.astro';
import SvelteRail from '../svelte/Rail.svelte';
import AstroRail from '../astro/Rail.astro';
import SvelteDock from '../svelte/Dock.svelte';
import AstroDock from '../astro/Dock.astro';
import SvelteLanguageSwitcher from '../svelte/LanguageSwitcher.svelte';
import AstroLanguageSwitcher from '../astro/LanguageSwitcher.astro';
import SvelteCopySurface from '../svelte/CopySurface.svelte';
import AstroCopySurface from '../astro/CopySurface.astro';
import SvelteProfileMenu from '../svelte/ProfileMenu.svelte';
import SveltePopover from '../svelte/Popover.svelte';

function icon(name: IconName, size: number, extraClass = ''): string {
  const cls = extraClass ? `bb-icon ${extraClass}` : 'bb-icon';
  const body = normalise(icons[name]);
  return (
    `<svg class="${cls}" viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" ` +
    `stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`
  );
}

describe('TextLink variants', () => {
  contracts(SvelteTextLink, AstroTextLink, [
    {
      name: 'arrow link carries the go tone and a trailing arrow',
      props: { href: '/valorant-stats', label: 'Learn more', variant: 'arrow', tone: 'go' },
      html:
        '<a class="bb-link bb-link--arrow bb-link--go" href="/valorant-stats">Learn more' +
        '<span class="bb-link__arrow" aria-hidden="true">→</span></a>',
    },
    {
      name: 'prose arrow keeps a caller class for composition',
      props: { href: '/import', label: 'Moving from another bot?', variant: 'arrow', tone: 'go', prose: true, class: 'migrate' },
      html:
        '<a class="bb-link bb-link--arrow bb-link--go bb-link--prose migrate" href="/import">Moving from another bot?' +
        '<span class="bb-link__arrow" aria-hidden="true">→</span></a>',
    },
    {
      name: 'inline link wraps its children',
      props: { href: 'https://example.test/me', variant: 'inline', external: true },
      slots: { default: 'Mavey' },
      html: '<a class="bb-link bb-link--inline" href="https://example.test/me" target="_blank" rel="noopener noreferrer">Mavey</a>',
    },
    {
      name: 'quiet back link leads with an icon',
      props: { href: '/modules', label: 'All modules', variant: 'quiet', icon: 'arrowLeft' },
      html: `<a class="bb-link bb-link--quiet" href="/modules">${icon('arrowLeft', 14, 'bb-link__icon')}All modules</a>`,
    },
    {
      name: 'TextLink quiet touch target',
      props: { href: '/modules', variant: 'quiet', touch: true, label: 'All modules' },
      html: '<a class="bb-link bb-link--quiet bb-link--touch" href="/modules">All modules</a>',
    },
  ]);

  test('rolling link with an icon keeps its glyph mask', async () => {
    const props = { href: '/modules', label: 'Back', icon: 'arrowLeft' };
    const html = svelteHtml(SvelteTextLink, props);
    expect(await astroHtml(AstroTextLink, props)).toBe(html);
    expect(html.startsWith(`<a class="bb-text-link" href="/modules" aria-label="Back">${icon('arrowLeft', 14, 'bb-text-link__icon')}`)).toBe(true);
    expect(html).toContain('<span class="bb-text-link__mask" aria-hidden="true">');
  });
});

describe('SectionNav modes', () => {
  contracts(SvelteSectionNav, AstroSectionNav, [
    {
      name: 'route mode marks the current page',
      props: {
        label: 'Server',
        items: [
          { href: '/discord/1', label: 'Overview', current: true },
          { href: '/discord/1/roles', label: 'Roles', current: false },
        ],
      },
      html:
        '<div class="bb-tabs-host"><nav class="bb-tabs bb-tabs--auto" aria-label="Server" data-lenis-prevent>' +
        '<a class="bb-tab is-active" href="/discord/1" aria-current="page">Overview</a>' +
        '<a class="bb-tab" href="/discord/1/roles">Roles</a></nav></div>',
    },
    {
      name: 'toc variant numbers its entries',
      props: {
        label: 'On this page',
        variant: 'toc',
        index: true,
        items: [
          { href: '#start', label: 'Start' },
          { href: '#next', label: 'Next', count: 3 },
        ],
      },
      html:
        '<div class="bb-tabs-host"><nav class="bb-tabs bb-tabs--toc" aria-label="On this page" data-lenis-prevent>' +
        '<a class="bb-tab" href="#start"><i class="bb-tab__index">01</i>Start</a>' +
        '<a class="bb-tab" href="#next"><i class="bb-tab__index">02</i>Next<span class="bb-tab__count">3</span></a>' +
        '</nav></div>',
    },
    {
      name: 'items pass their own attributes to the anchor',
      props: {
        label: 'Guide',
        variant: 'toc',
        items: [
          { href: '#setup', label: 'Setup', attrs: { 'data-guide-link': 'setup' } },
          { href: '#faq', label: 'FAQ' },
        ],
      },
      html:
        '<div class="bb-tabs-host"><nav class="bb-tabs bb-tabs--toc" aria-label="Guide" data-lenis-prevent>' +
        '<a class="bb-tab" href="#setup" data-guide-link="setup">Setup</a>' +
        '<a class="bb-tab" href="#faq">FAQ</a></nav></div>',
    },
  ]);
});

describe('Mark', () => {
  contracts(SvelteMark, AstroMark, [
    { name: 'solid is the bare contract', html: '<i class="bb-mark" aria-hidden="true"></i>' },
    {
      name: 'variant and size write the modifier and the hook',
      props: { variant: 'hollow', size: '7px' },
      html: '<i class="bb-mark bb-mark--hollow" style="--mark-size: 7px;" aria-hidden="true"></i>',
    },
    {
      name: 'a caller style joins the size hook',
      props: { size: '8px', style: 'color: var(--bb-muted);' },
      html: '<i class="bb-mark" style="--mark-size: 8px; color: var(--bb-muted);" aria-hidden="true"></i>',
    },
  ]);
});

describe('AlertBanner', () => {
  contracts(SvelteAlertBanner, AstroAlertBanner, [
    {
      name: 'tip callout renders authored markup as its message',
      props: { tone: 'warm', variant: 'callout', role: 'note' },
      slots: { default: '<b>Tip</b> Save first.' },
      html:
        '<div class="bb-alert bb-alert--warm bb-alert--callout" role="note">' +
        '<span class="bb-alert__msg"><b>Tip</b> Save first.</span></div>',
    },
    {
      name: 'neutral callout',
      props: { tone: 'neutral', variant: 'callout', role: 'note' },
      html: '<div class="bb-alert bb-alert--neutral bb-alert--callout" role="note"><span class="bb-alert__msg"></span></div>',
    },
    {
      name: 'action link',
      props: { tone: 'warm', placement: 'top', role: 'status', cta: { label: 'Exit', href: '/admin' } },
      html:
        '<div class="bb-alert bb-alert--warm bb-alert--top" role="status"><span class="bb-alert__msg"></span>' +
        '<a class="bb-alert__action" href="/admin">Exit</a></div>',
    },
    {
      name: 'action through a POST form',
      props: { tone: 'warm', placement: 'top', role: 'status', cta: { label: 'Exit', formAction: '/auth/logout' } },
      html:
        '<div class="bb-alert bb-alert--warm bb-alert--top" role="status"><span class="bb-alert__msg"></span>' +
        '<form method="POST" action="/auth/logout"><button type="submit" class="bb-alert__action">Exit</button></form></div>',
    },
    {
      name: 'success callout',
      props: { tone: 'success', variant: 'callout', role: 'note' },
      slots: { default: '<b>In plain words</b>We keep nothing.' },
      html:
        '<div class="bb-alert bb-alert--success bb-alert--callout" role="note">' +
        '<span class="bb-alert__msg"><b>In plain words</b>We keep nothing.</span></div>',
    },
    {
      name: 'flush warning row',
      props: { tone: 'warning', flush: true, role: 'note' },
      slots: { default: 'Two commands are off.' },
      html: '<div class="bb-alert bb-alert--warning bb-alert--flush" role="note"><span class="bb-alert__msg">Two commands are off.</span></div>',
    },
    {
      name: 'stacked row with an action',
      props: { variant: 'danger', role: 'note', stack: true },
      slots: { default: 'Two conflicts.' },
      html: '<div class="bb-alert bb-alert--danger bb-alert--stack" role="note"><span class="bb-alert__msg">Two conflicts.</span></div>',
    },
    {
      name: 'adds the row modifier only when asked, in both adapters',
      props: { tone: 'warm', placement: 'top', row: 2, role: 'status' },
      html: '<div class="bb-alert bb-alert--warm bb-alert--top bb-alert--row-2" role="status"><span class="bb-alert__msg"></span></div>',
    },
    {
      name: 'the row modifier is absent unless asked',
      props: { tone: 'warm', placement: 'top' },
      html: '<div class="bb-alert bb-alert--warm bb-alert--top" role="alert"><span class="bb-alert__msg"></span></div>',
    },
  ]);
});

describe('ErrorScene actions', () => {
  const props = {
    status: 404,
    eyebrow: 'Lost',
    title: 'Not here',
    description: 'Gone.',
    primary: { label: 'Home', href: '/' },
    secondary: { label: 'Back', 'data-error-back': '/' },
  };

  contracts(SvelteErrorScene, AstroErrorScene, [{ name: 'primary link and secondary button', props }]);

  test('the actions render on the scene contract classes', () => {
    expect(svelteHtml(SvelteErrorScene, props)).toContain(
      '<div class="bb-error-scene__actions">' +
        '<a class="bb-error-scene__action bb-error-scene__action--primary" href="/">Home</a>' +
        '<button class="bb-error-scene__action bb-error-scene__action--quiet" type="button" data-error-back="/">Back</button>' +
        '</div>',
    );
  });

  test('no action props and no snippet render no actions row', () => {
    const html = svelteHtml(SvelteErrorScene, { status: 500, eyebrow: 'E', title: 'T', description: 'D' });
    expect(html).not.toContain('bb-error-scene__actions');
  });
});

describe('Modal viewer and InspectorSurface idle', () => {
  contracts(SvelteModal, AstroModal, [
    {
      name: 'viewer frames a stage with toolbar and hint',
      props: { open: true, variant: 'viewer', label: 'Diagram', toolbarLabel: 'Controls' },
      slots: { default: '<svg></svg>', toolbar: '<button>+</button>', hint: '<span>Drag to pan</span>' },
      html:
        '<div class="bb-modal bb-modal--viewer" data-overlay style="z-index: 200">' +
        '<button class="bb-modal__backdrop" type="button" aria-label="Close" data-cursor="quiet"></button>' +
        '<div class="bb-modal__card" role="dialog" aria-modal="true" tabindex="-1" aria-label="Diagram" data-lenis-prevent>' +
        '<div class="bb-modal__stage"><svg></svg></div>' +
        '<div class="bb-modal__toolbar" role="toolbar" aria-label="Controls"><button>+</button></div>' +
        '<div class="bb-modal__hint" aria-hidden="true"><span>Drag to pan</span></div>' +
        '</div></div>',
    },
  ]);

  test('a closed astro modal stays in the document, hidden', async () => {
    const html = await astroHtml(AstroModal, { open: false, variant: 'viewer', label: 'Diagram' });
    expect(html).toContain('<div class="bb-modal bb-modal--viewer" data-overlay style="z-index: 200" hidden>');
  });

  contracts(SvelteInspectorSurface, AstroInspectorSurface, [
    {
      name: 'idle keeps the docked panel with the close control hidden',
      props: { open: false, title: 'Quotes' },
      svelteProps: { onClose: () => {} },
      slots: { idle: '<p>Pick a quote</p>' },
      html:
        '<aside class="bb-surface bb-card bb-surface--docked bb-surface--idle" aria-label="Quotes">' +
        '<div class="bb-surface__head"><span class="bb-surface__tag bb-tag bb-tag--bare">Quotes</span>' +
        `<button class="bb-surface__close" type="button" aria-label="Close">${icon('x', 14)}</button></div>` +
        '<div class="bb-surface__body"><div class="bb-surface__idle"><p>Pick a quote</p></div></div></aside>',
    },
  ]);

  test('without an idle snippet a closed surface still renders nothing', () => {
    expect(svelteHtml(SvelteInspectorSurface, { open: false, title: 'Quotes', onClose: () => {} })).toBe('');
  });
});

describe('SkipLink and LanguageSwitcher', () => {
  const FLAG = '<span class="bb-lang-switch__flag" aria-hidden="true"></span>';
  const CHEVRON = icon('chevron', 12, 'bb-lang-switch__chevron');
  const CHECK = icon('check', 14, 'bb-lang-switch__check');
  const TRIGGER_EN = `<button type="button" class="bb-lang-switch__trigger" popovertarget="lang" aria-label="Language: English">${FLAG}<span class="bb-lang-switch__label" data-fit>EN</span>${CHEVRON}</button>`;
  const MENU = '<ul class="bb-lang-switch__menu" id="lang" popover="auto" role="list" aria-label="Language">';
  const options = [
    { code: 'en', label: 'EN', current: true, title: 'English' },
    { code: 'fr', label: 'FR' },
  ];
  const optionBody = (code: string, text: string) =>
    `${FLAG}<span class="bb-lang-switch__name" lang="${code}">${text}</span>${CHECK}</button></li>`;

  contracts(SvelteSkipLink, AstroSkipLink, [
    {
      name: 'skip link',
      props: { href: '#main-content', label: 'Skip to content' },
      html: '<a class="bb-skip-link" href="#main-content">Skip to content</a>',
    },
    {
      name: 'skip link falls back to the catalog label',
      props: { href: '#main-content' },
      html: '<a class="bb-skip-link" href="#main-content">Skip to content</a>',
    },
  ]);

  contracts(SvelteLanguageSwitcher, AstroLanguageSwitcher, [
    {
      name: 'form mode posts the chosen code',
      props: { label: 'Language', action: '/lang', name: 'to', fields: { next: '/x' }, options, menuId: 'lang' },
      html:
        '<form method="POST" action="/lang" class="bb-lang-switch" data-bb-lang-switch><input type="hidden" name="next" value="/x">' +
        `${TRIGGER_EN}${MENU}` +
        `<li><button type="submit" name="to" value="en" class="bb-lang-switch__opt is-active" aria-pressed="true">${optionBody('en', 'English')}` +
        `<li><button type="submit" name="to" value="fr" class="bb-lang-switch__opt" aria-pressed="false">${optionBody('fr', 'FR')}` +
        '</ul></form>',
    },
    {
      name: 'language switcher falls back to the catalog label',
      props: { options: [{ code: 'en', label: 'EN', href: '/', current: true }], menuId: 'lang' },
      html:
        '<div class="bb-lang-switch" data-bb-lang-switch>' +
        `${TRIGGER_EN.replace('Language: English', 'Language: EN')}${MENU}` +
        `<li><a class="bb-lang-switch__opt is-active" href="/" hreflang="en" aria-current="true">${FLAG}<span class="bb-lang-switch__name" lang="en">EN</span>${CHECK}</a></li>` +
        '</ul></div>',
    },
  ]);

  test('svelte callback mode renders plain buttons', () => {
    const html = svelteHtml(SvelteLanguageSwitcher, { label: 'Language', options, onSelect: () => {}, menuId: 'lang' });
    expect(html).toBe(
      `<div class="bb-lang-switch" data-bb-lang-switch>${TRIGGER_EN}${MENU}` +
        `<li><button type="button" class="bb-lang-switch__opt is-active" aria-pressed="true">${optionBody('en', 'English')}` +
        `<li><button type="button" class="bb-lang-switch__opt" aria-pressed="false">${optionBody('fr', 'FR')}` +
        '</ul></div>',
    );
  });

  test.each([
    ['Rail', SvelteRail, AstroRail, { brand: { title: 'Bagel' }, groups: [] }],
    ['Dock', SvelteDock, AstroDock, { items: [] }],
  ])('%s landmark is never unnamed', async (_name, svelte, astro, props) => {
    expect(svelteHtml(svelte, props)).toContain('aria-label="Main navigation"');
    expect(await astroHtml(astro, props)).toContain('aria-label="Main navigation"');
  });
});

describe('Dock grouping', () => {
  const link = (href: string, label: string, extra: Record<string, unknown> = {}) => ({ href, label, ...extra });
  const board = {
    label: 'Board',
    items: [link('/', 'Overview', { icon: 'overview' }), link('/commands', 'Commands', { icon: 'commands', count: 4 }), link('/modules', 'Modules')],
  };
  const ops = { label: 'Ops', items: [link('/audit', 'Audit', { icon: 'audit', current: true })] };
  const dock = (props: Record<string, unknown>) => bothHtml(SvelteDock, AstroDock, props);
  const buttons = (html: string) => [...html.matchAll(/<button[^>]*>/g)].map((match) => match[0]);
  const badges = (html: string) => [...html.matchAll(/bb-dock-item__count" aria-hidden="true">(\d+)</g)].map((match) => match[1]);

  test('one group is a flat dock, several fold', async () => {
    const flat = await dock({ groups: [board], items: [link('/flat', 'Flat')] });
    expect(flat).toContain('<a class="bb-dock-item" href="/flat">');
    expect(flat).not.toContain('bb-dock__group');
    expect(await dock({ groups: [board, ops] })).toContain('bb-dock__group');
  });

  test('the home route is hoisted out of whichever group holds it', async () => {
    const inner = '<div class="bb-dock__inner">';
    expect(await dock({ groups: [board, ops] })).toContain(`${inner}<a class="bb-dock-item" href="/">`);
    expect(await dock({ groups: [board, ops], homeHref: '/audit' })).toContain(`${inner}<a class="bb-dock-item" href="/audit"`);
    const plain = { label: 'Plain', items: [link('/p', 'P'), link('/q', 'Q')] };
    const other = { label: 'Other', items: [link('/o', 'O'), link('/r', 'R')] };
    expect(await dock({ groups: [plain, other] })).toContain(`${inner}<div class="bb-dock__group">`);
  });

  test('a group the hoist empties is dropped, not rendered as an empty popover', async () => {
    const html = await dock({ groups: [{ label: 'Home', items: [link('/', 'Overview')] }, ops] });
    expect(html).toContain('Overview');
    expect(html).not.toContain('Home');
    expect(html).not.toContain('bb-dock__group');
  });

  test('a folded group is current when any page inside it is', async () => {
    const quiet = { label: 'Quiet', items: [link('/p', 'P'), link('/q', 'Q')] };
    const live = { label: 'Live', items: [link('/o', 'O', { current: true }), link('/r', 'R')] };
    expect(buttons(await dock({ groups: [quiet, live] })).map((button) => button.includes('data-active'))).toEqual([false, true]);
  });

  test('counts sum, and a zero sum shows no badge', async () => {
    const counted = { label: 'Counted', items: [link('/a', 'A', { count: 1 }), link('/b', 'B', { count: 3 })] };
    const zero = { label: 'Zero', items: [link('/c', 'C', { count: 0 }), link('/d', 'D')] };
    expect(badges(await dock({ groups: [counted, zero] }))).toEqual(['4']);
  });

  test('the group glyph falls back to the caller, never to a name from the bot', async () => {
    const bare = { label: 'Bare', items: [link('/x', 'X'), link('/y', 'Y')] };
    const drawn = { label: 'Drawn', items: [link('/z', 'Z', { icon: 'audit' }), link('/w', 'W')] };
    expect(await dock({ groups: [bare, drawn] })).not.toContain(normalise(icons.list));
    const html = await dock({ groups: [bare, drawn], fallbackIcon: 'list' });
    expect(html.split(normalise(icons.list))).toHaveLength(2);
    expect(html).toContain(normalise(icons.audit));
  });
});

describe('CopySurface', () => {
  const STATUS = '</button><span class="bb-copy__status" role="status"></span></span>';

  contracts(SvelteCopySurface, AstroCopySurface, [
    {
      name: 'card with label and chip hint',
      props: { text: 'BAGEL', label: 'Creator code', hint: 'Click to copy', copiedLabel: 'Copied' },
      html:
        '<span class="bb-copy-host"><button class="bb-copy bb-copy--card" type="button" data-copy="BAGEL">' +
        '<span class="bb-copy__label">Creator code</span><span class="bb-copy__row">' +
        '<span class="bb-copy__value">BAGEL</span><span class="bb-copy__hint" aria-hidden="true">' +
        `<span class="bb-copy__idle">Click to copy</span><span class="bb-copy__done">Copied</span></span></span>${STATUS}`,
    },
    {
      name: 'well shows glyphs and carries its timing',
      props: { text: '/mod bot', variant: 'well', hint: 'Copy', copiedLabel: 'Copied', flashMs: 2000, legacyFallback: true },
      html:
        '<span class="bb-copy-host"><button class="bb-copy bb-copy--well" type="button" data-copy="/mod bot" data-copy-ms="2000" data-copy-legacy>' +
        '<span class="bb-copy__row"><span class="bb-copy__value">/mod bot</span><span class="bb-copy__hint" aria-hidden="true">' +
        `<span class="bb-copy__idle">${icon('copy', 12)}Copy</span><span class="bb-copy__done">${icon('check', 12)}Copied</span>` +
        `</span></span>${STATUS}`,
    },
    {
      name: 'announce text rides on the surface for the status region',
      props: { text: '!hi', variant: 'row', hint: '', copiedLabel: 'Copied', announce: 'Copied !hi' },
      html:
        '<span class="bb-copy-host"><button class="bb-copy bb-copy--row" type="button" data-copy="!hi" data-copy-announce="Copied !hi">' +
        '<span class="bb-copy__row"><span class="bb-copy__value">!hi</span><span class="bb-copy__hint" aria-hidden="true">' +
        `<span class="bb-copy__idle"></span><span class="bb-copy__done">Copied</span></span></span>${STATUS}`,
    },
  ]);

  test('row children receive the copied state', () => {
    const children = createRawSnippet((copied: () => boolean) => ({ render: () => `<span>${copied()}</span>` }));
    const html = normalise(
      render(SvelteCopySurface, { props: { text: '!hi', variant: 'row', copiedLabel: 'Copied', children } }).body,
    );
    expect(html).toContain('<span class="bb-copy__value"><span>false</span></span>');
    expect(html).toContain('class="bb-copy bb-copy--row"');
  });
});

const avatar = createRawSnippet((state: () => { size: number; item: boolean }) => ({
  render: () => `<i data-size="${state().size}" data-item="${state().item}"></i>`,
}));

describe('ProfileMenu', () => {
  const base = { name: 'Mavey', caption: 'Owner', logoutLabel: 'Log out', avatar };

  test('topbar trigger names the account', () => {
    expect(svelteHtml(SvelteProfileMenu, base)).toBe(
      '<button class="bb-profile-topbar__operator" type="button" title="Mavey · Owner" aria-label="Mavey · Owner" ' +
        'aria-expanded="false" aria-haspopup="menu"><span class="bb-profile-topbar__avatar"><i data-size="30" data-item="false"></i></span>' +
        '<span class="bb-profile-topbar__op-id"><b>Mavey</b><i>Owner</i></span></button>',
    );
  });

  test('topbar menu lists dashboards, the exit and the logout', () => {
    const html = svelteHtml(SvelteProfileMenu, {
      ...base,
      open: true,
      links: [{ href: '/c/alpha', label: 'Alpha' }],
      linksLabel: 'Dashboards',
      exit: { href: '/', label: 'Back to mine' },
    });
    expect(html).toContain('<div class="bb-profile__scrim" role="presentation"></div>');
    expect(html).toContain('role="menu" aria-label="Account menu">');
    expect(html).toContain('<div class="bb-profile__head" aria-hidden="true"><span class="bb-profile__portrait"><i data-size="72" data-item="false"></i></span><b>Mavey</b><i>Owner</i>');
    expect(html).toContain('<div class="bb-profile__section" aria-hidden="true">Dashboards</div>');
    expect(html).toContain('<a class="bb-profile__link" href="/c/alpha" role="menuitem"><span class="bb-profile__avatar"><i data-size="26" data-item="true"></i></span>');
    expect(html).toContain(`<span class="bb-profile__avatar">${icon('home', 14)}</span><span class="bb-profile__name">Back to mine</span>`);
    expect(html).toContain('<form method="POST" action="/auth/logout" role="none"><button type="submit" class="bb-profile-topbar__op-menu-item" role="menuitem">Log out</button></form>');
  });

  test('rail without dashboards shows help, a static account row and the logout', () => {
    const html = svelteHtml(SvelteProfileMenu, {
      ...base,
      variant: 'rail',
      help: [{ href: 'mailto:help@example.test', label: 'Support', hint: 'help@example.test', icon: 'link' }],
      helpLabel: 'Support',
      feedback: { href: 'https://example.test/issues', label: 'Feedback', external: true },
    });
    expect(html.startsWith('<div class="bb-profile-rail__side-foot"><div class="bb-profile-rail__help">')).toBe(true);
    expect(html).toContain('aria-haspopup="menu">Support</button>');
    expect(html).toContain('<a class="bb-profile-rail__help-btn" href="https://example.test/issues" target="_blank" rel="noopener noreferrer">Feedback</a>');
    expect(html).toContain('<div class="bb-profile-rail__account" role="group">');
    expect(html).toContain('<form class="bb-profile-rail__logout" method="POST" action="/auth/logout">');
    expect(html).not.toContain('bb-profile-rail__account--btn');
  });

  test('rail with dashboards turns the account row into the menu trigger', () => {
    const html = svelteHtml(SvelteProfileMenu, { ...base, variant: 'rail', links: [{ href: '/c/alpha', label: 'Alpha' }] });
    expect(html).toContain('<button class="bb-profile-rail__account bb-profile-rail__account--btn" type="button" aria-expanded="false" aria-haspopup="menu">');
    expect(html).toContain(`<span class="bb-profile-rail__chev" aria-hidden="true">${icon('chevron', 14)}</span>`);
  });
});

describe('Popover', () => {
  const base = { label: 'Install the app', title: 'Add to home screen', closeLabel: 'Close', pill: snippet('<span>Install</span>') };

  test('closed popover is the pill alone', () => {
    expect(svelteHtml(SveltePopover, base)).toBe(
      '<div class="bb-popover"><div class="bb-popover__pill"><button class="bb-popover__cta" type="button" ' +
        'aria-label="Install the app" aria-haspopup="dialog" aria-expanded="false"><span>Install</span></button></div></div>',
    );
  });

  test('open popover labels its sheet by its heading and offers a dismiss', () => {
    const html = svelteHtml(SveltePopover, {
      ...base,
      open: true,
      dismissLabel: 'Dismiss',
      onDismiss: () => {},
      children: snippet('<ol></ol>'),
    });
    const id = /aria-labelledby="([^"]+)"/.exec(html)?.[1];
    expect(id).toBeTruthy();
    expect(html).toContain(`<h2 class="bb-h bb-h--l6" id="${id}">Add to home screen</h2>`);
    expect(html).toContain('<button class="bb-popover__x" type="button" aria-label="Dismiss">');
    expect(html).toContain('role="dialog" aria-modal="false"');
    expect(html).toContain('<ol></ol></div></div>');
  });

  test('bottom placement is an opt-in modifier class', () => {
    expect(svelteHtml(SveltePopover, { ...base, placement: 'bottom' })).toContain('<div class="bb-popover bb-popover--bottom">');
  });

  test('a pill that does not expand carries no popup state', () => {
    const html = svelteHtml(SveltePopover, { ...base, expands: false });
    expect(html).not.toContain('aria-haspopup');
    expect(html).not.toContain('aria-expanded');
  });
});

const STATIC_FLOW: [string, string][] = [
  ['elements/shell.css', '.bb-rail'],
  ['elements/shell.css', '.bb-topbar'],
  ['elements/surface.css', '.bb-surface--docked'],
  ['elements/editor-footer.css', '.bb-editor-foot'],
  ['elements/modal.css', '.bb-modal'],
];

const NO_FALLBACK_FILES = [
  'elements/text-link.css',
  'elements/alert.css',
  'elements/error-scene.css',
  'elements/profile-menu.css',
  'elements/modal.css',
  'elements/shell.css',
  'elements/editor-footer.css',
  'elements/search-input.css',
  'elements/skip-link.css',
  'elements/popover.css',
  'elements/copy-surface.css',
];

describe('navigation and feedback stylesheets', () => {
  sourceContracts([
    {
      name: 'the size hook defaults to the old 5px diamond',
      checks: [{ file: 'styles/elements/mark.css', has: ['.bb-mark { width: var(--mark-size, 5px); height: var(--mark-size, 5px);'] }],
    },
    {
      name: 'the arrow link does not reuse the rail sidebar class',
      checks: [{ file: 'styles/elements/text-link.css', lacks: [/\.bb-rail\b/] }],
    },
    {
      name: 'flush comes after callout so it clears both margins',
      checks: [
        {
          file: 'styles/elements/alert.css',
          has: [/\.bb-alert--success \{[^}]*--alert-rgb: var\(--bb-green-glow-rgb\)/],
          after: [['.bb-alert--flush {', '.bb-alert--callout {']],
        },
      ],
    },
    {
      name: 'stack gives the message its own row on phones so the action drops below it',
      checks: [
        {
          file: 'styles/elements/alert.css',
          has: [/@media \(max-width: 560px\) \{\s*\.bb-alert--stack \{\s*flex-wrap: wrap;\s*\}\s*\.bb-alert--stack \.bb-alert__msg \{\s*flex-basis: 100%;/],
        },
      ],
    },
    {
      name: 'viewer chrome has light rules on paper and ink tokens',
      checks: [
        {
          file: 'styles/elements/modal.css',
          has: [
            /:root\[data-theme="light"\] \.bb-modal--viewer \.bb-modal__backdrop \{[^}]*--bb-paper-warm-rgb/,
            /:root\[data-theme="light"\] \.bb-modal__toolbar \.bb-btn--icon \{[^}]*--bb-ink-soft/,
            /:root\[data-theme="light"\] \.bb-modal__hint \{[^}]*--bb-ink-rgb/,
          ],
        },
      ],
    },
    {
      name: 'bottom placement anchors above the pill with touch areas on the controls',
      checks: [
        {
          file: 'styles/elements/popover.css',
          has: [
            '.bb-popover--bottom {',
            'flex-direction: column-reverse',
            'bottom: calc(env(safe-area-inset-bottom, 0px) + 108px)',
            'inset: -7px 0',
            '.bb-popover--bottom :is(.bb-popover__cta, .bb-popover__x, .bb-popover__close)::after',
          ],
        },
      ],
    },
    ...STATIC_FLOW.map(([file, selector]) => ({
      name: `${file} renders ${selector} in flow under data-static`,
      checks: [{ file: `styles/${file}`, has: [`:is([data-static] ${selector}, ${selector}[data-static])`] }],
    })),
    ...NO_FALLBACK_FILES.map((file) => ({
      name: `${file} restates no token value as a fallback`,
      checks: [
        {
          file: `styles/${file}`,
          lacks: [/var\(--bb-[\w-]+,\s*[#\d]/, /var\(--bb-[\w-]+,\s*rgba?\(/, '#cf8a78'],
        },
      ],
    })),
  ]);
});
