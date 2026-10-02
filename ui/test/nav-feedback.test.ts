// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
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

type Adapter = unknown;
type Props = Record<string, unknown>;
type Slots = Record<string, string>;

const snippet = (html: string) => createRawSnippet(() => ({ render: () => html }));

function icon(name: IconName, size: number, extraClass = ''): string {
  const cls = extraClass ? `bb-icon ${extraClass}` : 'bb-icon';
  const body = normalise(icons[name]);
  return (
    `<svg class="${cls}" viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" ` +
    `stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`
  );
}

function svelteHtml(component: Adapter, props: Props, slots: Slots = {}): string {
  const snippets = Object.fromEntries(
    Object.entries(slots).map(([name, html]) => [name === 'default' ? 'children' : name, snippet(html)]),
  );
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return normalise(render(component as any, { props: { ...props, ...snippets } }).body);
}

async function astroHtml(component: Adapter, props: Props, slots: Slots = {}): Promise<string> {
  const container = await experimental_AstroContainer.create();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return normalise(await container.renderToString(component as any, { props, slots }));
}

interface Case {
  name: string;
  svelte: Adapter;
  astro: Adapter;
  props: Props;
  slots?: Slots;
  svelteProps?: Props;
  html?: string;
}

function contract({ name, svelte, astro, props, slots, svelteProps, html }: Case) {
  test(name, async () => {
    const fromSvelte = svelteHtml(svelte, { ...props, ...svelteProps }, slots);
    const fromAstro = await astroHtml(astro, props, slots);
    expect(fromSvelte).toBe(fromAstro);
    if (html !== undefined) expect(fromSvelte).toBe(html);
  });
}

describe('TextLink variants', () => {
  contract({
    name: 'arrow link carries the go tone and a trailing arrow',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: '/valorant-stats', label: 'Learn more', variant: 'arrow', tone: 'go' },
    html:
      '<a class="bb-link bb-link--arrow bb-link--go" href="/valorant-stats">Learn more' +
      '<span class="bb-link__arrow" aria-hidden="true">→</span></a>',
  });

  contract({
    name: 'prose arrow keeps a caller class for composition',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: '/import', label: 'Moving from another bot?', variant: 'arrow', tone: 'go', prose: true, class: 'migrate' },
    html:
      '<a class="bb-link bb-link--arrow bb-link--go bb-link--prose migrate" href="/import">Moving from another bot?' +
      '<span class="bb-link__arrow" aria-hidden="true">→</span></a>',
  });

  contract({
    name: 'inline link wraps its children',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: 'https://example.test/me', variant: 'inline', external: true },
    slots: { default: 'Mavey' },
    html:
      '<a class="bb-link bb-link--inline" href="https://example.test/me" target="_blank" rel="noopener noreferrer">Mavey</a>',
  });

  contract({
    name: 'quiet back link leads with an icon',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: '/modules', label: 'All modules', variant: 'quiet', icon: 'arrowLeft' },
    html: `<a class="bb-link bb-link--quiet" href="/modules">${icon('arrowLeft', 14, 'bb-link__icon')}All modules</a>`,
  });

  contract({
    name: 'rolling link accepts a leading icon',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: '/modules', label: 'Back', icon: 'arrowLeft' },
  });

  test('rolling link with an icon keeps its glyph mask', () => {
    const html = svelteHtml(SvelteTextLink, { href: '/modules', label: 'Back', icon: 'arrowLeft' });
    expect(html.startsWith(`<a class="bb-text-link" href="/modules" aria-label="Back">${icon('arrowLeft', 14, 'bb-text-link__icon')}`)).toBe(true);
    expect(html).toContain('<span class="bb-text-link__mask" aria-hidden="true">');
  });
});

describe('SectionNav modes', () => {
  contract({
    name: 'route mode marks the current page',
    svelte: SvelteSectionNav,
    astro: AstroSectionNav,
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
  });

  contract({
    name: 'toc variant numbers its entries',
    svelte: SvelteSectionNav,
    astro: AstroSectionNav,
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
  });

  contract({
    name: 'items pass their own attributes to the anchor',
    svelte: SvelteSectionNav,
    astro: AstroSectionNav,
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
  });
});

describe('Mark', () => {
  contract({
    name: 'solid is the bare contract',
    svelte: SvelteMark,
    astro: AstroMark,
    props: {},
    html: '<i class="bb-mark" aria-hidden="true"></i>',
  });

  contract({
    name: 'variant and size write the modifier and the hook',
    svelte: SvelteMark,
    astro: AstroMark,
    props: { variant: 'hollow', size: '7px' },
    html: '<i class="bb-mark bb-mark--hollow" style="--mark-size: 7px;" aria-hidden="true"></i>',
  });

  contract({
    name: 'a caller style joins the size hook',
    svelte: SvelteMark,
    astro: AstroMark,
    props: { size: '8px', style: 'color: var(--bb-muted);' },
    html: '<i class="bb-mark" style="--mark-size: 8px; color: var(--bb-muted);" aria-hidden="true"></i>',
  });

  test('the size hook defaults to the old 5px diamond', async () => {
    const tags = await Bun.file(new URL('../styles/elements/mark.css', import.meta.url)).text();
    expect(tags).toContain('.bb-mark { width: var(--mark-size, 5px); height: var(--mark-size, 5px);');
  });
});

describe('AlertBanner additions', () => {
  contract({
    name: 'tip callout renders authored markup as its message',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { tone: 'warm', variant: 'callout', role: 'note' },
    slots: { default: '<b>Tip</b> Save first.' },
    html:
      '<div class="bb-alert bb-alert--warm bb-alert--callout" role="note">' +
      '<span class="bb-alert__msg"><b>Tip</b> Save first.</span></div>',
  });

  contract({
    name: 'neutral callout',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { tone: 'neutral', variant: 'callout', role: 'note' },
    html: '<div class="bb-alert bb-alert--neutral bb-alert--callout" role="note"><span class="bb-alert__msg"></span></div>',
  });

  contract({
    name: 'action link',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { tone: 'warm', placement: 'top', role: 'status', cta: { label: 'Exit', href: '/admin' } },
    html:
      '<div class="bb-alert bb-alert--warm bb-alert--top" role="status"><span class="bb-alert__msg"></span>' +
      '<a class="bb-alert__action" href="/admin">Exit</a></div>',
  });

  contract({
    name: 'action through a POST form',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { tone: 'warm', placement: 'top', role: 'status', cta: { label: 'Exit', formAction: '/auth/logout' } },
    html:
      '<div class="bb-alert bb-alert--warm bb-alert--top" role="status"><span class="bb-alert__msg"></span>' +
      '<form method="POST" action="/auth/logout"><button type="submit" class="bb-alert__action">Exit</button></form></div>',
  });
});

describe('ErrorScene actions', () => {
  const ACTIONS =
    '<div class="bb-error-scene__actions">' +
    '<a class="bb-error-scene__action bb-error-scene__action--primary" href="/">Home</a>' +
    '<button class="bb-error-scene__action bb-error-scene__action--quiet" type="button" data-error-back="/">Back</button>' +
    '</div>';

  const props = {
    status: 404,
    eyebrow: 'Lost',
    title: 'Not here',
    description: 'Gone.',
    primary: { label: 'Home', href: '/' },
    secondary: { label: 'Back', 'data-error-back': '/' },
  };

  contract({ name: 'primary link and secondary button', svelte: SvelteErrorScene, astro: AstroErrorScene, props });

  test('the actions render on the scene contract classes', () => {
    expect(svelteHtml(SvelteErrorScene, props)).toContain(ACTIONS);
  });

  test('no action props and no snippet render no actions row', () => {
    const html = svelteHtml(SvelteErrorScene, { status: 500, eyebrow: 'E', title: 'T', description: 'D' });
    expect(html).not.toContain('bb-error-scene__actions');
  });
});

describe('Modal viewer', () => {
  contract({
    name: 'viewer frames a stage with toolbar and hint',
    svelte: SvelteModal,
    astro: AstroModal,
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
  });

  test('a closed astro modal stays in the document, hidden', async () => {
    const html = await astroHtml(AstroModal, { open: false, variant: 'viewer', label: 'Diagram' });
    expect(html).toContain('<div class="bb-modal bb-modal--viewer" data-overlay style="z-index: 200" hidden>');
  });
});

describe('InspectorSurface idle', () => {
  contract({
    name: 'idle keeps the docked panel with the close control hidden',
    svelte: SvelteInspectorSurface,
    astro: AstroInspectorSurface,
    props: { open: false, title: 'Quotes' },
    svelteProps: { onClose: () => {} },
    slots: { idle: '<p>Pick a quote</p>' },
    html:
      '<aside class="bb-surface bb-card bb-surface--docked bb-surface--idle" aria-label="Quotes">' +
      '<div class="bb-surface__head"><span class="bb-surface__tag bb-tag bb-tag--bare">Quotes</span>' +
      `<button class="bb-surface__close" type="button" aria-label="Close">${icon('x', 14)}</button></div>` +
      '<div class="bb-surface__body"><div class="bb-surface__idle"><p>Pick a quote</p></div></div></aside>',
  });

  test('without an idle snippet a closed surface still renders nothing', () => {
    expect(svelteHtml(SvelteInspectorSurface, { open: false, title: 'Quotes', onClose: () => {} })).toBe('');
  });
});

describe('SkipLink', () => {
  contract({
    name: 'skip link',
    svelte: SvelteSkipLink,
    astro: AstroSkipLink,
    props: { href: '#main-content', label: 'Skip to content' },
    html: '<a class="bb-skip-link" href="#main-content">Skip to content</a>',
  });
});

describe('catalog defaults', () => {
  contract({
    name: 'skip link falls back to the catalog label',
    svelte: SvelteSkipLink,
    astro: AstroSkipLink,
    props: { href: '#main-content' },
    html: '<a class="bb-skip-link" href="#main-content">Skip to content</a>',
  });

  contract({
    name: 'language switcher falls back to the catalog label',
    svelte: SvelteLanguageSwitcher,
    astro: AstroLanguageSwitcher,
    props: { options: [{ code: 'en', label: 'EN', href: '/', current: true }], menuId: 'lang' },
    html:
      '<div class="bb-lang-switch" data-bb-lang-switch>' +
      '<button type="button" class="bb-lang-switch__trigger" popovertarget="lang" aria-label="Language: EN"><span class="bb-lang-switch__flag" aria-hidden="true"></span><span class="bb-lang-switch__label" data-fit>EN</span><svg class="bb-icon bb-lang-switch__chevron" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9l6 6l6-6"></svg></button>' +
      '<ul class="bb-lang-switch__menu" id="lang" popover="auto" role="list" aria-label="Language">' +
      '<li><a class="bb-lang-switch__opt is-active" href="/" hreflang="en" aria-current="true"><span class="bb-lang-switch__flag" aria-hidden="true"></span><span class="bb-lang-switch__name" lang="en">EN</span><svg class="bb-icon bb-lang-switch__check" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6L9 17l-5-5"></svg></a></li>' +
      '</ul></div>',
  });

  test.each([
    ['Rail', SvelteRail, AstroRail, { brand: { title: 'Bagel' }, groups: [] }],
    ['Dock', SvelteDock, AstroDock, { items: [] }],
  ])('%s landmark is never unnamed', async (_name, svelte, astro, props) => {
    expect(svelteHtml(svelte, props)).toContain('aria-label="Main navigation"');
    expect(await astroHtml(astro, props)).toContain('aria-label="Main navigation"');
  });
});

describe('CopySurface', () => {
  contract({
    name: 'card with label and chip hint',
    svelte: SvelteCopySurface,
    astro: AstroCopySurface,
    props: { text: 'BAGEL', label: 'Creator code', hint: 'Click to copy', copiedLabel: 'Copied' },
    html:
      '<span class="bb-copy-host"><button class="bb-copy bb-copy--card" type="button" data-copy="BAGEL">' +
      '<span class="bb-copy__label">Creator code</span><span class="bb-copy__row">' +
      '<span class="bb-copy__value">BAGEL</span><span class="bb-copy__hint" aria-hidden="true">' +
      '<span class="bb-copy__idle">Click to copy</span><span class="bb-copy__done">Copied</span></span></span>' +
      '</button><span class="bb-copy__status" role="status"></span></span>',
  });

  contract({
    name: 'well shows glyphs and carries its timing',
    svelte: SvelteCopySurface,
    astro: AstroCopySurface,
    props: { text: '/mod bot', variant: 'well', hint: 'Copy', copiedLabel: 'Copied', flashMs: 2000, legacyFallback: true },
    html:
      '<span class="bb-copy-host"><button class="bb-copy bb-copy--well" type="button" data-copy="/mod bot" data-copy-ms="2000" data-copy-legacy>' +
      '<span class="bb-copy__row"><span class="bb-copy__value">/mod bot</span><span class="bb-copy__hint" aria-hidden="true">' +
      `<span class="bb-copy__idle">${icon('copy', 12)}Copy</span><span class="bb-copy__done">${icon('check', 12)}Copied</span>` +
      '</span></span></button><span class="bb-copy__status" role="status"></span></span>',
  });

  contract({
    name: 'announce text rides on the surface for the status region',
    svelte: SvelteCopySurface,
    astro: AstroCopySurface,
    props: { text: '!hi', variant: 'row', hint: '', copiedLabel: 'Copied', announce: 'Copied !hi' },
    html:
      '<span class="bb-copy-host"><button class="bb-copy bb-copy--row" type="button" data-copy="!hi" data-copy-announce="Copied !hi">' +
      '<span class="bb-copy__row"><span class="bb-copy__value">!hi</span><span class="bb-copy__hint" aria-hidden="true">' +
      '<span class="bb-copy__idle"></span><span class="bb-copy__done">Copied</span></span></span>' +
      '</button><span class="bb-copy__status" role="status"></span></span>',
  });

  test('small print sizes are hooks with the old defaults', async () => {
    const read = (path: string) => Bun.file(new URL(`../styles/${path}`, import.meta.url)).text();
    expect(await read('elements/copy-surface.css')).toContain('font-size: var(--copy-label-size, 10.5px);');
    expect(await read('elements/table.css')).toContain('font-size: var(--tbl-head-size, 11px);');
    expect(await read('elements/typography.css')).toContain('font-size: var(--label-mono-size, 10px);');
    expect(await read('elements/typography.css')).toContain('font-size: var(--h-label-size, 10px);');
    expect(await read('elements/badge.css')).toContain('font-size: var(--badge-pill-size, 10px);');
    expect(await read('elements/tabs.css')).toContain('.bb-tabs--wrap > .bb-tab { flex-shrink: 0; }');
  });

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
    const html = normalise(render(SvelteProfileMenu, { props: base }).body);
    expect(html).toBe(
      '<button class="bb-profile-topbar__operator" type="button" title="Mavey · Owner" aria-label="Mavey · Owner" ' +
        'aria-expanded="false" aria-haspopup="menu"><span class="bb-profile-topbar__avatar"><i data-size="30" data-item="false"></i></span>' +
        '<span class="bb-profile-topbar__op-id"><b>Mavey</b><i>Owner</i></span></button>',
    );
  });

  test('topbar menu lists dashboards, the exit and the logout', () => {
    const html = normalise(
      render(SvelteProfileMenu, {
        props: {
          ...base,
          open: true,
          links: [{ href: '/c/alpha', label: 'Alpha' }],
          linksLabel: 'Dashboards',
          exit: { href: '/', label: 'Back to mine' },
        },
      }).body,
    );
    expect(html).toContain('<div class="bb-profile__scrim" role="presentation"></div>');
    expect(html).toContain('role="menu" aria-label="Account menu">');
    expect(html).toContain('<div class="bb-profile__head" aria-hidden="true"><span class="bb-profile__portrait"><i data-size="72" data-item="false"></i></span><b>Mavey</b><i>Owner</i>');
    expect(html).toContain('<div class="bb-profile__section" aria-hidden="true">Dashboards</div>');
    expect(html).toContain('<a class="bb-profile__link" href="/c/alpha" role="menuitem"><span class="bb-profile__avatar"><i data-size="26" data-item="true"></i></span>');
    expect(html).toContain(`<span class="bb-profile__avatar">${icon('home', 14)}</span><span class="bb-profile__name">Back to mine</span>`);
    expect(html).toContain('<form method="POST" action="/auth/logout" role="none"><button type="submit" class="bb-profile-topbar__op-menu-item" role="menuitem">Log out</button></form>');
  });

  test('rail without dashboards shows help, a static account row and the logout', () => {
    const html = normalise(
      render(SvelteProfileMenu, {
        props: {
          ...base,
          variant: 'rail',
          help: [{ href: 'mailto:help@example.test', label: 'Support', hint: 'help@example.test', icon: 'link' }],
          helpLabel: 'Support',
          feedback: { href: 'https://example.test/issues', label: 'Feedback', external: true },
        },
      }).body,
    );
    expect(html.startsWith('<div class="bb-profile-rail__side-foot"><div class="bb-profile-rail__help">')).toBe(true);
    expect(html).toContain('aria-haspopup="menu">Support</button>');
    expect(html).toContain('<a class="bb-profile-rail__help-btn" href="https://example.test/issues" target="_blank" rel="noopener noreferrer">Feedback</a>');
    expect(html).toContain('<div class="bb-profile-rail__account" role="group">');
    expect(html).toContain('<form class="bb-profile-rail__logout" method="POST" action="/auth/logout">');
    expect(html).not.toContain('bb-profile-rail__account--btn');
  });

  test('rail with dashboards turns the account row into the menu trigger', () => {
    const html = normalise(
      render(SvelteProfileMenu, { props: { ...base, variant: 'rail', links: [{ href: '/c/alpha', label: 'Alpha' }] } }).body,
    );
    expect(html).toContain('<button class="bb-profile-rail__account bb-profile-rail__account--btn" type="button" aria-expanded="false" aria-haspopup="menu">');
    expect(html).toContain(`<span class="bb-profile-rail__chev" aria-hidden="true">${icon('chevron', 14)}</span>`);
  });
});

describe('Popover', () => {
  const base = { label: 'Install the app', title: 'Add to home screen', closeLabel: 'Close', pill: snippet('<span>Install</span>') };

  test('closed popover is the pill alone', () => {
    const html = normalise(render(SveltePopover, { props: base }).body);
    expect(html).toBe(
      '<div class="bb-popover"><div class="bb-popover__pill"><button class="bb-popover__cta" type="button" ' +
        'aria-label="Install the app" aria-haspopup="dialog" aria-expanded="false"><span>Install</span></button></div></div>',
    );
  });

  test('open popover labels its sheet by its heading and offers a dismiss', () => {
    const html = normalise(
      render(SveltePopover, {
        props: { ...base, open: true, dismissLabel: 'Dismiss', onDismiss: () => {}, children: snippet('<ol></ol>') },
      }).body,
    );
    const id = /aria-labelledby="([^"]+)"/.exec(html)?.[1];
    expect(id).toBeTruthy();
    expect(html).toContain(`<h2 class="bb-h bb-h--l6" id="${id}">Add to home screen</h2>`);
    expect(html).toContain('<button class="bb-popover__x" type="button" aria-label="Dismiss">');
    expect(html).toContain('role="dialog" aria-modal="false"');
    expect(html).toContain('<ol></ol></div></div>');
  });

  test('bottom placement is an opt-in modifier class', () => {
    const html = normalise(render(SveltePopover, { props: { ...base, placement: 'bottom' } }).body);
    expect(html).toContain('<div class="bb-popover bb-popover--bottom">');
  });

  test('bottom placement anchors above the pill with touch areas on the controls', async () => {
    const css = await Bun.file(new URL('../styles/elements/popover.css', import.meta.url)).text();
    expect(css).toContain('.bb-popover--bottom {');
    expect(css).toContain('flex-direction: column-reverse');
    expect(css).toContain('bottom: calc(env(safe-area-inset-bottom, 0px) + 108px)');
    expect(css).toContain('inset: -7px 0');
    expect(css).toContain('.bb-popover--bottom :is(.bb-popover__cta, .bb-popover__x, .bb-popover__close)::after');
  });

  test('a pill that does not expand carries no popup state', () => {
    const html = normalise(render(SveltePopover, { props: { ...base, expands: false } }).body);
    expect(html).not.toContain('aria-haspopup');
    expect(html).not.toContain('aria-expanded');
  });
});

describe('stylesheet contracts', () => {
  const read = (path: string) => Bun.file(new URL(`../styles/${path}`, import.meta.url)).text();

  test('the arrow link does not reuse the rail sidebar class', async () => {
    expect(await read('elements/text-link.css')).not.toMatch(/\.bb-rail\b/);
  });

  test.each([
    ['elements/shell.css', '.bb-rail'],
    ['elements/shell.css', '.bb-topbar'],
    ['elements/surface.css', '.bb-surface--docked'],
    ['elements/editor-footer.css', '.bb-editor-foot'],
    ['elements/modal.css', '.bb-modal'],
  ])('%s renders %s in flow under data-static', async (file, selector) => {
    expect(await read(file)).toContain(`:is([data-static] ${selector}, ${selector}[data-static])`);
  });

  test.each([
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
  ])('%s restates no token value as a fallback', async (file) => {
    const css = await read(file);
    expect(css).not.toMatch(/var\(--bb-[\w-]+,\s*[#\d]/);
    expect(css).not.toMatch(/var\(--bb-[\w-]+,\s*rgba?\(/);
    expect(css).not.toContain('#cf8a78');
  });
});
