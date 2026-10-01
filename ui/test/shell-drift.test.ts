// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterEach, describe, expect, test } from 'bun:test';
import { astroHtml, contracts, snippet, sourceContracts, svelteHtml } from './contract';
import { icons } from '../lib/icons';
import { menuKeys } from '../lib/menu-keys';
import SvelteAppShell from '../svelte/AppShell.svelte';
import AstroAppShell from '../astro/AppShell.astro';
import SvelteNavProgress from '../svelte/NavProgress.svelte';
import AstroNavProgress from '../astro/NavProgress.astro';
import SvelteProfileMenu from '../svelte/ProfileMenu.svelte';

describe('ProfileMenu help group', () => {
  const base = { name: 'Mavey', caption: 'Owner', logoutLabel: 'Log out', avatar: snippet('<i></i>'), open: true };

  test('topbar renders the titled group with hinted lines and name-only new-tab links', () => {
    const html = svelteHtml(SvelteProfileMenu, {
      ...base,
      helpTitle: 'Help',
      newTabLabel: 'opens in new tab',
      help: [{ href: 'mailto:a@b.c', label: 'Support', hint: 'a@b.c', icon: 'link' }],
      more: [{ href: 'https://s.test', label: 'Status', icon: 'pulse', external: true }],
    });
    expect(html).toContain('<div class="bb-profile-topbar__op-dash-group" role="group" aria-label="Help"><div class="bb-profile__section" aria-hidden="true">Help</div>');
    expect(html).toContain('<span class="bb-profile__hint" title="a@b.c">a@b.c</span>');
    expect(html).toContain('target="_blank" rel="noopener noreferrer"');
    expect(html).toContain('<span class="bb-profile__name">Status <span class="bb-sr-only">opens in new tab</span></span>');
    expect(html.indexOf('bb-profile__section" aria-hidden="true">Help')).toBeLessThan(html.indexOf('<form'));
  });

  test('topbar without help lines or more links has no help group', () => {
    expect(svelteHtml(SvelteProfileMenu, { ...base, helpTitle: 'Help' })).not.toContain('Help');
  });

  test('a link that stays in the tab has no screen-reader suffix', () => {
    const html = svelteHtml(SvelteProfileMenu, { ...base, newTabLabel: 'opens in new tab', more: [{ href: '/x', label: 'Local' }] });
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
  const props = { brand: { title: 'B', sub: 's', href: '/' }, skipLabel: 'Skip' };

  test('adds the stacked modifier only when asked, in both adapters', async () => {
    const off = svelteHtml(SvelteAppShell, props, { default: 'x' });
    const on = svelteHtml(SvelteAppShell, { ...props, stacked: true }, { default: 'x' });
    expect(off).not.toContain('bb-shell--stacked');
    expect(on).toContain('<div class="bb-shell bb-shell--stacked">');
    expect(await astroHtml(AstroAppShell, props)).not.toContain('bb-shell--stacked');
    expect(await astroHtml(AstroAppShell, { ...props, stacked: true })).toContain('<div class="bb-shell bb-shell--stacked">');
  });
});

describe('NavProgress', () => {
  const bar = (active: boolean) => ({
    name: `active=${active} matches across adapters`,
    props: { active },
    html: `<div class="bb-nav-progress${active ? ' bb-nav-progress--active' : ''}" role="presentation" aria-hidden="true"></div>`,
  });

  contracts(SvelteNavProgress, AstroNavProgress, [bar(false), bar(true)]);
});

describe('icons', () => {
  test('arrowUpRight is generated', () => {
    expect(icons.arrowUpRight).toContain('<path');
  });
});

describe('stylesheet contracts', () => {
  sourceContracts([
    {
      name: 'nav-progress is compositor only, respects reduced motion and restates no fallback',
      checks: [
        {
          file: 'styles/elements/nav-progress.css',
          has: ['transform: scaleX(0.9)', 'transform 6s var(--bb-ease-out-expo)', /prefers-reduced-motion: reduce[\s\S]*scaleX\(1\)/],
          lacks: [/var\(--bb-[\w-]+,/],
        },
      ],
    },
    {
      name: 'shell, alert and profile menu carry the rules console.css used to hold',
      checks: [
        { file: 'styles/elements/shell.css', has: ['.bb-shell--stacked .bb-topbar'] },
        { file: 'styles/elements/alert.css', has: [/\.bb-alert--row-2 \{\s*top: 44px;/] },
        {
          file: 'styles/elements/profile-menu.css',
          has: ['max-height: calc(100dvh - 96px)', /pointer: coarse[\s\S]*min-height: 44px[\s\S]*inset: -4px/],
        },
        { file: 'styles/elements/status-dot.css', has: [/\.bb-status-dot\.flat \{\s*box-shadow: none;/] },
      ],
    },
  ]);
});
