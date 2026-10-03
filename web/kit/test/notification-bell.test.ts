// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { render } from 'svelte/server';
import NotificationBell from '../components/NotificationBell.svelte';

const props = {
  notifications: [],
  viewAllHref: '/notifications',
  title: 'Notifications',
  viewAllLabel: 'View all',
  emptyLabel: 'Nothing yet.',
  unreadLabel: (count: number) => `Unread: ${count}`,
  levelLabels: { info: 'Info', success: 'Success', warning: 'Warning', critical: 'Critical' },
};

function withoutComments(html: string): string {
  let previous: string;
  do {
    previous = html;
    html = html.replace(/<!--[\s\S]*?-->/g, '');
  } while (html !== previous);
  return html;
}

const closed = (extra: Record<string, unknown> = {}) =>
  withoutComments(render(NotificationBell, { props: { ...props, ...extra } as never }).body);

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8');

describe('NotificationBell trigger', () => {
  test('is a disclosure button for a non-modal dialog, not a menu', () => {
    const html = closed();
    expect(html).toContain('type="button"');
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain('aria-haspopup="dialog"');
    expect(html).not.toContain('aria-controls');
    expect(html).not.toContain('menu');
  });

  test('the accessible name carries the unread count', () => {
    expect(closed({ unreadCount: 3 })).toContain('aria-label="Notifications, Unread: 3"');
    expect(closed({ unreadCount: 0 })).toContain('aria-label="Notifications"');
  });
});

describe('NotificationBell source contract', () => {
  const source = read('../components/NotificationBell.svelte');
  const css = read('../styles/notifications.css');

  test.each([
    ['the panel is a labelled non-modal dialog the trigger controls', ['role="dialog"', 'aria-modal="false"', 'aria-labelledby={titleId}', 'aria-controls={open ? panelId : undefined}']],
    ['focus moves in on open and returns to the trigger on Escape', ['if (open && panel) focusWithin(panel);', "event.key === 'Escape'", 'close(true)', 'trigger?.focus()']],
    ['focus leaving the widget closes it', ['onfocusout', '!wrap?.contains(next)']]
  ] as [string, string[]][])('%s', (_name, snippets) => {
    for (const snippet of snippets) expect(source).toContain(snippet);
  });

  test('the trigger widens its hit area on coarse pointers', () => {
    expect(css).toMatch(/pointer: coarse[\s\S]*bb-notifications__icon-btn::after/);
  });
});

describe('bell locale keys', () => {
  test.each(['en', 'fr'])('%s carries the unread and level strings', (locale) => {
    const bell = JSON.parse(read(`../../../locales/${locale}/console/bell.json`));
    for (const key of ['unread', 'levelInfo', 'levelSuccess', 'levelWarning', 'levelCritical']) {
      expect(typeof bell[key]).toBe('string');
    }
    expect(bell.unread).toContain('{count}');
  });
});
