// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterEach, describe, expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { createRawSnippet } from 'svelte';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import { icons } from '../lib/icons';
import { menuKeys } from '../lib/menu-keys';
import SvelteAlertBanner from '../svelte/AlertBanner.svelte';
import AstroAlertBanner from '../astro/AlertBanner.astro';
import SvelteAppShell from '../svelte/AppShell.svelte';
import AstroAppShell from '../astro/AppShell.astro';
import SvelteNavProgress from '../svelte/NavProgress.svelte';
import AstroNavProgress from '../astro/NavProgress.astro';
import SvelteProfileMenu from '../svelte/ProfileMenu.svelte';
import SvelteStatusDot from '../svelte/StatusDot.svelte';
import AstroStatusDot from '../astro/StatusDot.astro';

const read = (path: string) => Bun.file(new URL(`../styles/${path}`, import.meta.url)).text();
const avatar = createRawSnippet(() => ({ render: () => '<i></i>' }));
const children = createRawSnippet(() => ({ render: () => 'x' }));

async function astro(component: unknown, props: Record<string, unknown>) {
  const container = await experimental_AstroContainer.create();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return normalise(await container.renderToString(component as any, { props }));
}

describe('ProfileMenu help group', () => {
  const base = { name: 'Mavey', caption: 'Owner', logoutLabel: 'Log out', avatar, open: true };

  test('topbar renders the titled group with hinted lines and name-only new-tab links', () => {
    const html = normalise(
      render(SvelteProfileMenu, {
        props: {
          ...base,
          helpTitle: 'Help',
          newTabLabel: 'opens in new tab',
          help: [{ href: 'mailto:a@b.c', label: 'Support', hint: 'a@b.c', icon: 'link' }],
          more: [{ href: 'https://s.test', label: 'Status', icon: 'pulse', external: true }],
        },
      }).body,
    );
    expect(html).toContain('<div class="bb-profile-topbar__op-dash-group" role="group" aria-label="Help"><div class="bb-profile__section" aria-hidden="true">Help</div>');
    expect(html).toContain('<span class="bb-profile__hint" title="a@b.c">a@b.c</span>');
    expect(html).toContain('target="_blank" rel="noopener noreferrer"');
    expect(html).toContain('<span class="bb-profile__name">Status <span class="bb-sr-only">opens in new tab</span></span>');
    expect(html.indexOf('bb-profile__section" aria-hidden="true">Help')).toBeLessThan(html.indexOf('<form'));
  });

  test('topbar without help lines or more links has no help group', () => {
    const html = normalise(render(SvelteProfileMenu, { props: { ...base, helpTitle: 'Help' } }).body);
    expect(html).not.toContain('Help');
  });

  test('a link that stays in the tab has no screen-reader suffix', () => {
    const html = normalise(
      render(SvelteProfileMenu, { props: { ...base, newTabLabel: 'opens in new tab', more: [{ href: '/x', label: 'Local' }] } }).body,
    );
    expect(html).toContain('<span class="bb-profile__name">Local</span>');
  });
});

describe('menuKeys', () => {
  const realDocument = globalThis.document;
  afterEach(() => {
    globalThis.document = realDocument;
  });

  function fixture() {
    const fake = { activeElement: undefined as unknown };
    const items: { id: string; focus: () => void }[] = ['a', 'b', 'c'].map((id) => ({
      id,
      focus: () => void (fake.activeElement = items.find((i) => i.id === id)),
    }));
    globalThis.document = fake as unknown as Document;
    let listener: ((event: KeyboardEvent) => void) | undefined;
    const node = {
      querySelectorAll: () => items,
      addEventListener: (_: string, fn: (event: KeyboardEvent) => void) => (listener = fn),
      removeEventListener: () => (listener = undefined),
    } as unknown as HTMLElement;
    const press = (key: string) => {
      let prevented = false;
      listener?.({ key, preventDefault: () => (prevented = true) } as unknown as KeyboardEvent);
      return prevented;
    };
    return { items, fake, node, press, has: () => listener !== undefined };
  }

  test('focuses the first item on mount', () => {
    const f = fixture();
    menuKeys(f.node, () => {});
    expect(f.fake.activeElement).toBe(f.items[0]);
  });

  test('arrows wrap, Home and End jump, and the keys are consumed', () => {
    const f = fixture();
    menuKeys(f.node, () => {});
    expect(f.press('ArrowUp')).toBe(true);
    expect(f.fake.activeElement).toBe(f.items[2]);
    f.press('ArrowDown');
    expect(f.fake.activeElement).toBe(f.items[0]);
    f.press('End');
    expect(f.fake.activeElement).toBe(f.items[2]);
    f.press('Home');
    expect(f.fake.activeElement).toBe(f.items[0]);
    expect(f.press('a')).toBe(false);
  });

  test('Tab calls back without being consumed and destroy detaches', () => {
    const f = fixture();
    let tabs = 0;
    const action = menuKeys(f.node, () => tabs++);
    expect(f.press('Tab')).toBe(false);
    expect(tabs).toBe(1);
    action.destroy();
    expect(f.has()).toBe(false);
  });
});

describe('AppShell stacked', () => {
  const brand = { title: 'B', sub: 's', href: '/' };
  const props = { brand, skipLabel: 'Skip' };

  test('adds the stacked modifier only when asked, in both adapters', async () => {
    const off = normalise(render(SvelteAppShell, { props: { ...props, children } }).body);
    const on = normalise(render(SvelteAppShell, { props: { ...props, stacked: true, children } }).body);
    expect(off).not.toContain('bb-shell--stacked');
    expect(on).toContain('<div class="bb-shell bb-shell--stacked">');
    expect(await astro(AstroAppShell, props)).not.toContain('bb-shell--stacked');
    expect(await astro(AstroAppShell, { ...props, stacked: true })).toContain('<div class="bb-shell bb-shell--stacked">');
  });
});

describe('AlertBanner second row', () => {
  test('adds the row modifier only when asked, in both adapters', async () => {
    const props = { tone: 'warm', placement: 'top', row: 2, role: 'status' } as const;
    const expected = '<div class="bb-alert bb-alert--warm bb-alert--top bb-alert--row-2" role="status"><span class="bb-alert__msg"></span></div>';
    expect(normalise(render(SvelteAlertBanner, { props }).body)).toBe(expected);
    expect(await astro(AstroAlertBanner, props)).toBe(expected);
    expect(normalise(render(SvelteAlertBanner, { props: { tone: 'warm', placement: 'top' } }).body)).not.toContain('row-2');
  });
});

describe('StatusDot flat', () => {
  test.each([false, true])('flat=%p matches across adapters', async (flat) => {
    const expected = `<span class="bb-status-dot warning${flat ? ' flat' : ''}" aria-hidden="true"></span>`;
    expect(normalise(render(SvelteStatusDot, { props: { tone: 'warning', flat } }).body)).toBe(expected);
    expect(await astro(AstroStatusDot, { tone: 'warning', flat })).toBe(expected);
  });
});

describe('NavProgress', () => {
  test.each([false, true])('active=%p matches across adapters', async (active) => {
    const expected = `<div class="bb-nav-progress${active ? ' bb-nav-progress--active' : ''}" role="presentation" aria-hidden="true"></div>`;
    expect(normalise(render(SvelteNavProgress, { props: { active } }).body)).toBe(expected);
    expect(await astro(AstroNavProgress, { active })).toBe(expected);
  });
});

describe('icons', () => {
  test('arrowUpRight is generated', () => {
    expect(icons.arrowUpRight).toContain('<path');
  });
});

describe('stylesheet contracts', () => {
  test('nav-progress is compositor only, respects reduced motion and restates no fallback', async () => {
    const css = await read('elements/nav-progress.css');
    expect(css).toContain('transform: scaleX(0.9)');
    expect(css).toContain('transform 6s var(--bb-ease-out-expo)');
    expect(css).toMatch(/prefers-reduced-motion: reduce[\s\S]*scaleX\(1\)/);
    expect(css).not.toMatch(/var\(--bb-[\w-]+,/);
  });

  test('shell, alert and profile menu carry the rules console.css used to hold', async () => {
    expect(await read('elements/shell.css')).toContain('.bb-shell--stacked .bb-topbar');
    expect(await read('elements/alert.css')).toMatch(/\.bb-alert--row-2 \{\s*top: 44px;/);
    const menu = await read('elements/profile-menu.css');
    expect(menu).toContain('max-height: calc(100dvh - 96px)');
    expect(menu).toMatch(/pointer: coarse[\s\S]*min-height: 44px[\s\S]*inset: -4px/);
    expect(await read('elements/status-dot.css')).toMatch(/\.bb-status-dot\.flat \{\s*box-shadow: none;/);
  });
});
