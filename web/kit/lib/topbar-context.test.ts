// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { staticText } from './i18n/static';
import { buildConsoleContext, consoleAccountRole, type ConsoleContextInput, type ConsoleRoleInput } from './topbar-context';

function t(key: string, params?: Record<string, string | number>): string {
  const raw = staticText('en', key);
  if (!params) return raw;
  return raw.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? ''));
}

describe('buildConsoleContext', () => {
  test('returns undefined for an owner session without impersonation', () => {
    expect(buildConsoleContext(t, { login: 'mavey_tv' })).toBeUndefined();
  });

  test.each([
    [
      'shared access builds a warm context titled shared dashboard with a leave link',
      { delegateLogin: 'mavey_tv', sectionLabels: ['Commands', 'Timers', 'Quotes'] } satisfies ConsoleContextInput,
      {
        tone: 'warm',
        title: 'Shared dashboard',
        subject: 'mavey_tv',
        subjectPost: "'s dashboard",
        scopesTitle: 'Commands, Timers, Quotes',
        actions: [{ label: 'Leave shared', compactLabel: 'Exit', href: '/delegate/exit' }],
      },
    ],
    [
      'admin view builds a danger context with logout form action',
      { login: 'mavey_tv', impersonatorLogin: 'itsmavey' } satisfies ConsoleContextInput,
      {
        tone: 'danger',
        title: 'Admin impersonation',
        subject: 'mavey_tv',
        subjectPost: ' (admin itsmavey)',
        actions: [{ label: 'End view', compactLabel: 'Exit', formAction: '/auth/logout' }],
      },
    ],
    [
      'stacked session merges shared and admin into one compact danger context',
      {
        delegateLogin: 'mavey_tv',
        sectionLabels: ['Commands'],
        login: 'alex_mod',
        impersonatorLogin: 'itsmavey',
      } satisfies ConsoleContextInput,
      {
        tone: 'danger',
        title: 'Admin impersonation',
        subject: 'mavey_tv',
        actions: [
          { label: 'Leave shared', compactLabel: 'Exit', href: '/delegate/exit', subtle: true },
          { label: 'End view', compactLabel: 'Exit', formAction: '/auth/logout' },
        ],
      },
    ],
  ])('%s', (_label, input, expected) => {
    expect(buildConsoleContext(t, input)).toMatchObject(expected);
  });
});

describe('consoleAccountRole', () => {
  test.each([
    ['owner session keeps broadcaster role', { isDelegate: false } satisfies ConsoleRoleInput, 'Broadcaster'],
    ['delegated session clarifies signed-in identity', { isDelegate: true } satisfies ConsoleRoleInput, 'Signed in as you'],
    [
      'admin view labels the acting admin',
      { isDelegate: false, impersonatorLogin: 'itsmavey' } satisfies ConsoleRoleInput,
      'Admin: itsmavey',
    ],
  ])('%s', (_label, input, want) => {
    expect(consoleAccountRole(t, input)).toBe(want);
  });
});
