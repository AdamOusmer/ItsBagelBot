// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { consoleLocales, readConsoleTree } from '@bagel/kit/i18n/fs';
import {
  PAGE_ACTION,
  PAGE_DELAYED,
  PAGE_HINT,
  PAGE_HREF,
  PAGE_LABEL,
  PAGE_TOAST_OFF,
  PAGE_TOAST_ON,
  PUBLIC_PAGES,
  pageHiddenField,
  pageScope,
  pageSetVerb
} from './public-pages';

const TEXT_TABLES = [PAGE_LABEL, PAGE_HINT, PAGE_DELAYED, PAGE_TOAST_ON, PAGE_TOAST_OFF];

function resolves(tree: unknown, key: string): boolean {
  let node = tree;
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return false;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === 'string';
}

describe('public page registry', () => {
  test('every page names the wire field, verb and scope the users service publishes', () => {
    expect(PUBLIC_PAGES.map((page) => [pageHiddenField(page), pageSetVerb(page), pageScope(page)])).toEqual([
      ['commands_page_hidden', 'commands_page_set', 'commands_page'],
      ['leaderboard_page_hidden', 'leaderboard_page_set', 'leaderboard_page'],
      ['songqueue_page_hidden', 'songqueue_page_set', 'songqueue_page']
    ]);
  });

  test('every page has its own settings action and a link on its public host', () => {
    expect(new Set(Object.values(PAGE_ACTION)).size).toBe(PUBLIC_PAGES.length);
    expect(PUBLIC_PAGES.map((page) => PAGE_HREF[page]('streamer'))).toEqual([
      'https://commands.itsbagelbot.com/user/streamer',
      'https://leaderboard.itsbagelbot.com/streamer',
      'https://queue.itsbagelbot.com/sr/streamer'
    ]);
  });

  test.each(consoleLocales())('every settings string a page row uses exists in %s', (locale) => {
    const tree = readConsoleTree(locale);
    const missing = TEXT_TABLES.flatMap((table) => PUBLIC_PAGES.map((page) => table[page]).filter((key) => !resolves(tree, key)));
    expect(missing).toEqual([]);
  });
});
