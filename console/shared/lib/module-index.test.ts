// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  categoryAnchorId,
  categoryHref,
  categorySlug,
  countByCategory,
  filterModuleIndex,
  groupModulesByCategory,
  moduleCommandChips,
  moduleHref,
  moduleMatchesQuery,
  MODULE_CATEGORY_ORDER,
  orderedCategories,
  parseStatusFilter,
  readModuleIndexQuery,
  writeModuleIndexQuery,
  type ModuleIndexQuery
} from './module-index';
import { MODULE_CATALOG, moduleDef, type ModuleState } from './types';

function state(id: string, enabled = false): ModuleState {
  const def = moduleDef(id);
  if (!def) throw new Error(`missing catalog module ${id}`);
  return { def, enabled, config: {} };
}

function query(partial: Partial<ModuleIndexQuery> = {}): ModuleIndexQuery {
  return { q: '', category: '', status: 'all', ...partial };
}

describe('module index matching', () => {
  test('finds a module by the command chat actually types', () => {
    const song = moduleDef('songqueue');
    expect(song).toBeDefined();
    expect(moduleMatchesQuery(song!, '!sr')).toBe(true);
    expect(moduleMatchesQuery(song!, 'songrequest')).toBe(true);
    expect(moduleMatchesQuery(song!, 'spotify')).toBe(true);
  });

  test('finds a game pack by the game name, not its internal id', () => {
    const fn = moduleDef('fortnite');
    expect(fn).toBeDefined();
    expect(moduleMatchesQuery(fn!, 'fortnite')).toBe(true);
    expect(moduleMatchesQuery(fn!, '!fn')).toBe(true);
    expect(moduleMatchesQuery(fn!, 'songrequest')).toBe(false);
  });

  test('finds loyalty by a nested game command', () => {
    const loyalty = moduleDef('loyalty');
    expect(loyalty).toBeDefined();
    expect(moduleMatchesQuery(loyalty!, '!gamble')).toBe(true);
    expect(moduleMatchesQuery(loyalty!, '!duel')).toBe(true);
  });

  test('blank query matches everything', () => {
    expect(moduleMatchesQuery(moduleDef('timers')!, '   ')).toBe(true);
  });

  test('short compact queries do not substring-match unrelated modules', () => {
    expect(moduleMatchesQuery(moduleDef('fortnite')!, 'sr')).toBe(false);
  });
});

describe('module index filters', () => {
  const items = [state('timers', true), state('songqueue', false), state('fortnite', true)];

  test('status on keeps only enabled rows', () => {
    const shown = filterModuleIndex(items, query({ status: 'on' })).map((m) => m.def.id);
    expect(shown).toEqual(['timers', 'fortnite']);
  });

  test('drops nested children so they cannot be armed from the directory', () => {
    const nested = [state('loyalty'), state('gamble'), state('duel'), state('counters')];
    expect(filterModuleIndex(nested, query()).map((m) => m.def.id)).toEqual(['loyalty', 'counters']);
  });

  test('category and search compose without dropping catalog order', () => {
    const shown = filterModuleIndex(MODULE_CATALOG.map((def) => ({ def, enabled: false, config: {} })), query({
      q: 'stats',
      category: 'Stats'
    }));
    expect(shown.length).toBeGreaterThan(0);
    expect(shown.every((m) => m.def.category === 'Stats')).toBe(true);
  });
});

describe('module command chips', () => {
  test('caps at three and reports the overflow', () => {
    const song = moduleDef('songqueue')!;
    const { chips, extra } = moduleCommandChips(song, 3);
    expect(chips[0]).toBe('!sr');
    expect(chips.length).toBeLessThanOrEqual(3);
    expect(chips.length + extra).toBeGreaterThanOrEqual(chips.length);
  });

  test('promotes a reply command when the module has no command list', () => {
    const time = moduleDef('time')!;
    expect(time.commands).toBeUndefined();
    expect(moduleCommandChips(time).chips).toEqual(['!time']);
  });
});

describe('grouping and hrefs', () => {
  test('orders Chat before Stats even if Stats arrives first', () => {
    const items = [state('fortnite'), state('timers'), state('loyalty')];
    expect(groupModulesByCategory(items).map((g) => g.name)).toEqual([
      'Chat',
      'Points',
      'Stats'
    ]);
  });

  test('puts Moderation first so AutoMod is at the top of the directory', () => {
    const items = [state('timers'), state('automod'), state('fortnite')];
    expect(groupModulesByCategory(items).map((g) => g.name)).toEqual([
      'Moderation',
      'Chat',
      'Stats'
    ]);
  });

  test('appends a category the order list does not know', () => {
    expect(orderedCategories(['Stats', 'Weird', 'Chat', 'Moderation'])).toEqual([
      'Moderation',
      'Chat',
      'Stats',
      'Weird'
    ]);
  });

  test('counts faceted results per category', () => {
    const counts = countByCategory([state('timers'), state('triggers'), state('fortnite')]);
    expect(counts.Chat).toBe(2);
    expect(counts.Stats).toBe(1);
  });

  test('folds Song Requests and Govee into Gear', () => {
    const items = [state('govee'), state('songqueue'), state('raffle')];
    expect(groupModulesByCategory(items).map((g) => g.name)).toEqual(['Play', 'Gear']);
    expect(groupModulesByCategory(items).find((g) => g.name === 'Gear')?.modules.map((m) => m.def.id)).toEqual([
      'govee',
      'songqueue'
    ]);
  });

  test('href prefers a bespoke page over the generic inspector', () => {
    expect(moduleHref(moduleDef('songqueue')!)).toBe('/songqueue');
    expect(moduleHref(moduleDef('time')!)).toBe('/modules/time');
  });
});

describe('index query URL', () => {
  test('round-trips filters and drops defaults', () => {
    const url = new URL('https://console.test/modules?x=1');
    writeModuleIndexQuery(url, { q: '  !sr  ', category: 'Play', status: 'off' });
    expect(url.searchParams.get('q')).toBe('!sr');
    expect(url.searchParams.get('cat')).toBe('play');
    expect(url.searchParams.get('status')).toBe('off');
    const read = readModuleIndexQuery(url.searchParams, ['Chat', 'Play', 'Stats']);
    expect(read).toEqual({ q: '!sr', category: 'Play', status: 'off' });
    writeModuleIndexQuery(url, { q: '', category: '', status: 'all' });
    expect(url.search).toBe('?x=1');
  });

  test('category anchors are unique and prefixed so they cannot collide with a module id', () => {
    const ids = MODULE_CATEGORY_ORDER.map(categoryAnchorId);
    expect(new Set(ids).size).toBe(ids.length);
    expect(categoryAnchorId('Moderation')).toBe('cat-moderation');
    expect(categoryHref('Chat')).toBe('#cat-chat');
    expect(ids.every((id) => id.startsWith('cat-'))).toBe(true);
  });

  test('unknown status and slug collapse to the unfiltered view', () => {
    expect(parseStatusFilter('maybe')).toBe('all');
    expect(categorySlug('Chat Tools')).toBe('chat-tools');
    const read = readModuleIndexQuery(new URLSearchParams('cat=nope&status=yes'), ['Stats']);
    expect(read.category).toBe('');
    expect(read.status).toBe('all');
  });
});
