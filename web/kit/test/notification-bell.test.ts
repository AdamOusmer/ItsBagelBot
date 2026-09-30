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

const closed = (extra: Record<string, unknown> = {}) =>
  render(NotificationBell, { props: { ...props, ...extra } as never }).body.replace(/<!--[\s\S]*?-->/g, '');

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

  test('the panel is a labelled non-modal dialog the trigger controls', () => {
    expect(source).toContain('role="dialog"');
    expect(source).toContain('aria-modal="false"');
    expect(source).toContain('aria-labelledby={titleId}');
    expect(source).toContain('aria-controls={open ? panelId : undefined}');
  });

  test('focus moves in on open and returns to the trigger on Escape', () => {
    expect(source).toContain('if (open && panel) focusWithin(panel);');
    expect(source).toContain("event.key === 'Escape'");
    expect(source).toContain('close(true)');
    expect(source).toContain('trigger?.focus()');
  });

  test('focus leaving the widget closes it', () => {
    expect(source).toContain('onfocusout');
    expect(source).toContain('!wrap?.contains(next)');
  });

  test('no hard-coded English or dead scrim handler remains', () => {
    expect(source).not.toContain('Recent notifications');
    expect(source).not.toContain('Nothing yet');
    expect(source).not.toContain('View all');
    expect(source).not.toContain('onkeydown={(e)');
    expect(source).not.toContain('{n.level}</Badge>');
  });

  test('the heading level is a prop defaulting to the previous h4', () => {
    expect(source).toContain('headingLevel = 4');
    expect(source).toContain('this={`h${headingLevel}`}');
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
