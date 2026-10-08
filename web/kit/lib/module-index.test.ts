// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  categoryAnchorId,
  categoryHref,
  filterModuleIndex,
  groupModulesByCategory,
  moduleCommandChips,
  moduleHref,
  moduleMatchesQuery,
  MODULE_CATEGORY_ORDER,
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
  const listed = (id: string, q: string) => filterModuleIndex([state(id)], query({ q })).length === 1;

  test.each([
    { name: 'finds a module by the command chat actually types', id: 'songqueue', q: '!sr', want: true },
    { name: 'finds a module by its command word', id: 'songqueue', q: 'songrequest', want: true },
    { name: 'finds a module by the service it drives', id: 'songqueue', q: 'spotify', want: true },
    { name: 'finds a game pack by the game name, not its internal id', id: 'fortnite', q: 'fortnite', want: true },
    { name: 'finds a game pack by its command', id: 'fortnite', q: '!fn', want: true },
    { name: 'a game pack is not found by another module command', id: 'fortnite', q: 'songrequest', want: false },
    { name: 'finds loyalty by a nested game command', id: 'loyalty', q: '!gamble', want: true },
    { name: 'finds loyalty by another nested game command', id: 'loyalty', q: '!duel', want: true },
    { name: 'blank query matches everything', id: 'timers', q: '   ', want: true },
    { name: 'short compact queries do not substring-match unrelated modules', id: 'fortnite', q: 'sr', want: false }
  ])('$name', ({ id, q, want }) => {
    expect(listed(id, q)).toBe(want);
  });

  test('combines feature and command clues in any order, requiring every term', () => {
    const song = moduleDef('songqueue')!;
    expect(moduleMatchesQuery(song, 'Spotify song requests')).toBe(true);
    expect(moduleMatchesQuery(song, '!sr spotify')).toBe(true);
    expect(moduleMatchesQuery(song, 'songrequests spotify')).toBe(true);
    expect(moduleMatchesQuery(song, 'spotify fortnite')).toBe(false);
    expect(moduleMatchesQuery(moduleDef('fortnite')!, '!sr spotify')).toBe(false);
  });

  test('localized extra haystack finds CODM by copy that is not in the English catalog', () => {
    const def = moduleDef('codm')!;
    expect(moduleMatchesQuery(def, 'consultation', 'Consultation de profil Call of Duty: Mobile')).toBe(true);
    expect(moduleMatchesQuery(def, 'consultation')).toBe(false);
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
    expect(moduleCommandChips(song, 3)).toEqual({ chips: ['!sr', '!remove', '!skip'], extra: 3 });
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
    const weird: ModuleState = { ...state('timers'), def: { ...moduleDef('timers')!, category: 'Weird' } };
    const items = [state('fortnite'), weird, state('triggers'), state('automod')];
    expect(groupModulesByCategory(items).map((g) => g.name)).toEqual(['Moderation', 'Chat', 'Stats', 'Weird']);
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
    const read = readModuleIndexQuery(new URLSearchParams('cat=nope&status=yes'), ['Stats']);
    expect(read.category).toBe('');
    expect(read.status).toBe('all');
  });

  test('a category name with spaces round-trips through its slug', () => {
    expect(categoryAnchorId('Chat Tools')).toBe('cat-chat-tools');
    expect(readModuleIndexQuery(new URLSearchParams('cat=chat-tools'), ['Chat Tools']).category).toBe('Chat Tools');
  });
});
