// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The modules index is a chooser, not a gallery: streamers arrive knowing a
// job ("song requests") or a chat token ("!sr"), not a catalog category.
// Equal-weight cards hid on/off state (the .on/.off classes were never
// styled). A rAF scrollspy sidebar then vanished below 980px and on search,
// so a phone had no way to jump sections. Matching, faceting and grouping
// stay a pure function of the catalog so the page can only render; in-page
// jumps are hash links via categoryHref, not a second filter. A query like
// "!sr" keeps finding Song Requests if the tile copy is rewritten.

import type { ModuleDef, ModuleState } from './types';

export type ModuleStatusFilter = 'all' | 'on' | 'off';

export type ModuleIndexQuery = {
  q: string;
  // Catalog category name, or '' for every intent.
  category: string;
  status: ModuleStatusFilter;
};

export type ModuleCommandChips = {
  chips: string[];
  extra: number;
};

export type ModuleCategoryGroup = {
  name: string;
  modules: ModuleState[];
};

// Jobs, not buckets: Chat / Community / Games hid Song Requests behind
// "Community" and five stats packs behind one gamepad. Moderation first so
// AutoMod is the first row a streamer sees — it is on by default and the
// one module that should be configured before the rest of the catalog.
// Spotify and Govee share Gear: both are a third-party account you plug in
// before chat can drive the room; a one-item Lights section left Govee
// looking like an orphan. Stats last so they cannot bury timers.
// Categories absent from this list (a future catalog addition) append in
// first-seen order rather than being dropped.
export const MODULE_CATEGORY_ORDER = [
  'Moderation',
  'Chat',
  'Channel',
  'Points',
  'Play',
  'Gear',
  'Stats'
] as const;

export const MODULE_CATEGORY_I18N: Record<string, { label: string; hint: string }> = {
  Chat: { label: 'modules.catChat', hint: 'modules.catChatHint' },
  Channel: { label: 'modules.catChannel', hint: 'modules.catChannelHint' },
  Points: { label: 'modules.catPoints', hint: 'modules.catPointsHint' },
  Play: { label: 'modules.catPlay', hint: 'modules.catPlayHint' },
  Gear: { label: 'modules.catGear', hint: 'modules.catGearHint' },
  Moderation: { label: 'modules.catModeration', hint: 'modules.catModerationHint' },
  Stats: { label: 'modules.catStats', hint: 'modules.catStatsHint' }
};

export function categorySlug(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
}

// In-page target for a category heading. The cat- prefix keeps a future
// module id from colliding with a section jump (Settings uses bare #account
// because those ids are owned by one page).
export function categoryAnchorId(name: string): string {
  return `cat-${categorySlug(name)}`;
}

export function categoryHref(name: string): string {
  return `#${categoryAnchorId(name)}`;
}

export function parseStatusFilter(raw: string | null | undefined): ModuleStatusFilter {
  return raw === 'on' || raw === 'off' ? raw : 'all';
}

export function readModuleIndexQuery(
  params: URLSearchParams,
  categories: readonly string[]
): ModuleIndexQuery {
  const slug = params.get('cat') ?? '';
  const category = categories.find((c) => categorySlug(c) === slug) ?? '';
  return {
    q: params.get('q') ?? '',
    category,
    status: parseStatusFilter(params.get('status'))
  };
}

export function writeModuleIndexQuery(url: URL, query: ModuleIndexQuery): void {
  const q = query.q.trim();
  if (q) url.searchParams.set('q', q);
  else url.searchParams.delete('q');
  if (query.category) url.searchParams.set('cat', categorySlug(query.category));
  else url.searchParams.delete('cat');
  if (query.status !== 'all') url.searchParams.set('status', query.status);
  else url.searchParams.delete('status');
}

export function moduleHref(def: ModuleDef): string {
  return def.href ?? `/modules/${def.id}`;
}

// Leading token only: "!gamble <amount>" is one command, not a chip per word.
function commandToken(raw: string): string {
  const token = raw.trim().split(/\s+/)[0] ?? '';
  return token;
}

export function moduleCommandChips(def: ModuleDef, limit = 3): ModuleCommandChips {
  const seen = new Set<string>();
  const all: string[] = [];
  const add = (raw: string) => {
    const token = commandToken(raw);
    if (!token) return;
    const key = token.toLowerCase();
    if (seen.has(key)) return;
    seen.add(key);
    all.push(token);
  };
  for (const command of def.commands ?? []) {
    add(command.trigger);
  }
  for (const reply of def.replies) {
    if (reply.command) add(`!${reply.command}`);
  }
  return { chips: all.slice(0, limit), extra: Math.max(0, all.length - limit) };
}

export function moduleSearchHaystack(def: ModuleDef): string {
  const parts = [def.id, def.label, def.tagline, def.description, def.category];
  for (const command of def.commands ?? []) {
    parts.push(command.trigger, command.summary);
  }
  for (const reply of def.replies) {
    parts.push(reply.label, reply.event);
    if (reply.command) parts.push(`!${reply.command}`);
  }
  return parts.join('\n').toLowerCase();
}

export function moduleMatchesQuery(def: ModuleDef, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const hay = moduleSearchHaystack(def);
  if (hay.includes(q)) return true;
  // "songrequest" must still hit "Song Requests" after the ledger hid the
  // long aliases (!songrequest) so the detail page would not list every
  // spelling. Compact match is gated at 4 chars so "!sr" stays a token
  // match against the command list, not a substring of unrelated words.
  const compactHay = hay.replace(/[^a-z0-9]+/g, '');
  const compactQ = q.replace(/[^a-z0-9]+/g, '');
  return compactQ.length >= 4 && compactHay.includes(compactQ);
}

export function filterModuleIndex(
  items: readonly ModuleState[],
  query: ModuleIndexQuery
): ModuleState[] {
  return items.filter((item) => {
    if (query.category && item.def.category !== query.category) return false;
    if (query.status === 'on' && !item.enabled) return false;
    if (query.status === 'off' && item.enabled) return false;
    return moduleMatchesQuery(item.def, query.q);
  });
}

export function countByCategory(items: readonly ModuleState[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const item of items) {
    const key = item.def.category;
    out[key] = (out[key] ?? 0) + 1;
  }
  return out;
}

export function orderedCategories(
  present: readonly string[],
  order: readonly string[] = MODULE_CATEGORY_ORDER
): string[] {
  const set = new Set(present);
  const head = order.filter((name) => set.has(name));
  const tail = present.filter((name) => !order.includes(name));
  return [...head, ...tail];
}

export function groupModulesByCategory(
  items: readonly ModuleState[],
  order: readonly string[] = MODULE_CATEGORY_ORDER
): ModuleCategoryGroup[] {
  const by = new Map<string, ModuleState[]>();
  for (const item of items) {
    const list = by.get(item.def.category);
    if (list) list.push(item);
    else by.set(item.def.category, [item]);
  }
  return orderedCategories([...by.keys()], order).map((name) => ({
    name,
    modules: by.get(name) ?? []
  }));
}
