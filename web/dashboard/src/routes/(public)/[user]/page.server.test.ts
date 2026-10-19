// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';
import { boardHref, emptyCopyKeys } from '../../../lib/leaderboard-board';

stubSvelteKit({ dev: true });

type Row = { viewerId: string; viewerLogin: string; viewerName: string; points: number; watchSeconds: number; rank: number };
type Range = { from: number; to: number; total: number };
type Page = {
  podium: unknown[];
  rows: Row[];
  query: string;
  page: number;
  pages: number;
  range: Range;
  degraded: boolean;
  commandsPage: boolean;
};

type PageRequest = { limit: number; offset: number; query: string };

const board: Row[] = Array.from({ length: 87 }, (_, i) => ({
  viewerId: String(i + 1),
  viewerLogin: `v${i + 1}`,
  viewerName: '',
  points: 1000 - i,
  watchSeconds: 60,
  rank: i + 1
}));
const pageRequests: PageRequest[] = [];

let commandsPageReply: (id: string) => Promise<boolean> = async () => true;
let loyaltyFails = false;
let loyaltyEnabled = true;
let leaderboardPageReply: (id: string) => Promise<boolean> = async () => true;
let standingsReads = 0;
let viewerCount: number | null = null;

function activeBoard(): Row[] {
  return board.slice(0, viewerCount ?? board.length);
}

mock.module('$lib/server/loyalty-store', () => ({
  readLoyalty: async () => {
    if (loyaltyFails) throw new Error('loyalty down');
    return { enabled: loyaltyEnabled, config: { pointsName: 'bagels' } };
  },
  topStandings: async () => {
    standingsReads++;
    if (viewerCount !== null) return activeBoard().slice(0, 3);
    return [{ viewerId: '1', viewerLogin: 'fan', viewerName: 'Fan', points: 10, watchSeconds: 60 }];
  },
  standingsPage: async (_userId: string, req: PageRequest) => {
    pageRequests.push(req);
    const hits = activeBoard().filter((row) => row.viewerLogin.includes(req.query));
    return { rows: hits.slice(req.offset, req.offset + req.limit), total: hits.length };
  }
}));
mock.module('$lib/server/services', () => ({
  resolveLogin: async (login: string) => ({ userId: '42', username: login }),
  userCommandsPage: (id: string) => commandsPageReply(id),
  userLeaderboardPage: (id: string) => leaderboardPageReply(id)
}));

const { load } = await import('./+page.server');

type Outcome = { status?: number; message?: string; location?: string; edgeCache404?: boolean; page?: Page };

let lastLocals: Record<string, unknown> = {};

async function loadPage(search = ''): Promise<Outcome> {
  const locals: Record<string, unknown> = { locale: 'en' };
  lastLocals = locals;
  const event = {
    params: { user: 'somechannel' },
    url: new URL(`https://leaderboard.itsbagelbot.com/somechannel${search}`),
    locals
  };
  try {
    return { page: (await load(event as never)) as unknown as Page, edgeCache404: locals.edgeCache404 as boolean | undefined };
  } catch (e) {
    const err = e as { status?: number; location?: string; body?: { message?: string } };
    return { status: err.status, message: err.body?.message, location: err.location, edgeCache404: locals.edgeCache404 as boolean | undefined };
  }
}

type Setup = {
  commandsPage?: () => Promise<boolean>;
  leaderboardPage?: () => Promise<boolean>;
  loyaltyFails?: boolean;
  loyaltyEnabled?: boolean;
  viewers?: number;
};

function arrange(setup: Setup): void {
  pageRequests.length = 0;
  standingsReads = 0;
  viewerCount = setup.viewers ?? null;
  commandsPageReply = setup.commandsPage ?? (async () => true);
  leaderboardPageReply = setup.leaderboardPage ?? (async () => true);
  loyaltyFails = setup.loyaltyFails ?? false;
  loyaltyEnabled = setup.loyaltyEnabled ?? true;
}

const cases: {
  name: string;
  commandsPage: () => Promise<boolean>;
  loyaltyFails?: boolean;
  want: { top: number; degraded: boolean; commandsPage: boolean };
}[] = [
  {
    name: 'a visible commands page keeps the visit link',
    commandsPage: async () => true,
    want: { top: 1, degraded: false, commandsPage: true }
  },
  {
    name: 'a hidden commands page keeps standings and drops the visit link',
    commandsPage: async () => false,
    want: { top: 1, degraded: false, commandsPage: false }
  },
  {
    name: 'a rejected commands-page read fails open like the commands page',
    commandsPage: async () => {
      throw new Error('users service blip');
    },
    want: { top: 1, degraded: false, commandsPage: true }
  },
  {
    name: 'a hidden commands page does not hide a degraded standings notice',
    commandsPage: async () => false,
    loyaltyFails: true,
    want: { top: 0, degraded: true, commandsPage: false }
  }
];

describe('(public)/[user] load: commands-page toggle', () => {
  test.each(cases)('$name', async ({ commandsPage, loyaltyFails: fails, want }) => {
    arrange({ commandsPage, loyaltyFails: fails });
    const { page } = await loadPage();
    expect({ top: page!.podium.length, degraded: page!.degraded, commandsPage: page!.commandsPage }).toEqual(want);
  });
});

const notFound = { status: 404, message: 'Channel not found', edgeCache404: true };

describe('(public)/[user] load: leaderboard visibility', () => {
  test('a hidden leaderboard 404s edge-cached with the unknown-channel message and reads nothing else', async () => {
    arrange({ leaderboardPage: async () => false });
    const out = await loadPage();
    expect({ ...out }).toEqual(notFound);
    expect(standingsReads).toBe(0);
  });

  test('a disabled loyalty module 404s edge-cached', async () => {
    arrange({ loyaltyEnabled: false });
    expect(await loadPage()).toEqual(notFound);
    expect(standingsReads).toBe(0);
  });

  test('a rejected leaderboard-flag read fails open and renders', async () => {
    arrange({
      leaderboardPage: async () => {
        throw new Error('users service blip');
      }
    });
    const out = await loadPage();
    expect({ status: out.status, edgeCache404: out.edgeCache404, top: out.page?.podium.length }).toEqual({
      status: undefined,
      edgeCache404: undefined,
      top: 1
    });
  });

  test('a rejected loyalty read renders the degraded page instead of a 404', async () => {
    arrange({ loyaltyFails: true });
    const out = await loadPage();
    expect({ status: out.status, edgeCache404: out.edgeCache404, degraded: out.page?.degraded }).toEqual({
      status: undefined,
      edgeCache404: undefined,
      degraded: true
    });
  });
});

type View = { first: number | undefined; count: number; query: string; page: number; pages: number; range: Range };

function viewOf(page: Page): View {
  return { first: page.rows[0]?.rank, count: page.rows.length, query: page.query, page: page.page, pages: page.pages, range: page.range };
}

const boardCases: { name: string; search: string; want: View; requests: PageRequest[] }[] = [
  {
    name: 'the default board lists ranks 4 to 18 and counts pages after the podium',
    search: '',
    want: { first: 4, count: 15, query: '', page: 1, pages: 6, range: { from: 4, to: 18, total: 87 } },
    requests: [{ limit: 15, offset: 3, query: '' }]
  },
  {
    name: 'page 2 starts at rank 19',
    search: '?page=2',
    want: { first: 19, count: 15, query: '', page: 2, pages: 6, range: { from: 19, to: 33, total: 87 } },
    requests: [{ limit: 15, offset: 18, query: '' }]
  },
  {
    name: 'a page past the end falls back to the last page',
    search: '?page=99',
    want: { first: 79, count: 9, query: '', page: 6, pages: 6, range: { from: 79, to: 87, total: 87 } },
    requests: [
      { limit: 15, offset: 1473, query: '' },
      { limit: 15, offset: 78, query: '' }
    ]
  },
  {
    name: 'a search returns real board ranks from the first match, top three included',
    search: '?q=v1',
    want: { first: 1, count: 11, query: 'v1', page: 1, pages: 1, range: { from: 1, to: 11, total: 11 } },
    requests: [{ limit: 15, offset: 0, query: 'v1' }]
  },
  {
    name: 'a canonical search returns the matching ranks',
    search: '?q=v8',
    want: { first: 8, count: 9, query: 'v8', page: 1, pages: 1, range: { from: 1, to: 9, total: 9 } },
    requests: [{ limit: 15, offset: 0, query: 'v8' }]
  },
  {
    name: 'a search without hits reports an empty single page',
    search: '?q=zzzz',
    want: { first: undefined, count: 0, query: 'zzzz', page: 1, pages: 1, range: { from: 0, to: 0, total: 0 } },
    requests: [{ limit: 15, offset: 0, query: 'zzzz' }]
  },
  {
    name: 'a canonical search at the length cap is searched as is',
    search: `?q=${'a'.repeat(50)}`,
    want: { first: undefined, count: 0, query: 'a'.repeat(50), page: 1, pages: 1, range: { from: 0, to: 0, total: 0 } },
    requests: [{ limit: 15, offset: 0, query: 'a'.repeat(50) }]
  }
];

const redirectCases: { name: string; search: string; want: string }[] = [
  { name: 'an upper-case search lowercases', search: '?q=Otter', want: '/somechannel?q=otter' },
  { name: 'a padded search trims', search: '?q=+otter+', want: '/somechannel?q=otter' },
  { name: 'a leading at sign is dropped', search: '?q=%40Otter', want: '/somechannel?q=otter' },
  { name: 'inner whitespace collapses', search: '?q=a%20%20%09b', want: '/somechannel?q=a+b' },
  { name: 'an empty search drops the param', search: '?q=', want: '/somechannel' },
  { name: 'page 1 drops the param', search: '?page=1', want: '/somechannel' },
  { name: 'a zero-padded page normalizes', search: '?page=02', want: '/somechannel?page=2' },
  { name: 'a malformed page drops the param', search: '?page=abc', want: '/somechannel' },
  { name: 'a negative page drops the param', search: '?page=-3', want: '/somechannel' },
  { name: 'a page past the cap clamps to the cap', search: '?page=999', want: '/somechannel?page=399' },
  { name: 'other params survive', search: '?q=Otter&lang=fr&page=1', want: '/somechannel?lang=fr&q=otter' },
  { name: 'an over-long search is capped at 50 characters', search: `?q=${'A'.repeat(300)}`, want: `/somechannel?q=${'a'.repeat(50)}` }
];

describe('(public)/[user] load: canonical redirect', () => {
  test.each(redirectCases)('$name', async ({ search, want }) => {
    arrange({});
    const out = await loadPage(search);
    expect({ status: out.status, location: out.location }).toEqual({ status: 308, location: want });
    expect(pageRequests).toEqual([]);
  });

  test.each(redirectCases)('the target of "$name" does not redirect again', async ({ want }) => {
    arrange({});
    const out = await loadPage(want.slice('/somechannel'.length));
    expect(out.page).toBeDefined();
  });

  test('a degraded board is flagged uncacheable', async () => {
    arrange({ loyaltyFails: true });
    await loadPage('?q=v1');
    expect(lastLocals.edgeCacheSkip).toBe(true);
  });

  test('a healthy board is not flagged', async () => {
    arrange({});
    await loadPage('?q=v1');
    expect(lastLocals.edgeCacheSkip).toBeUndefined();
  });
});

describe('(public)/[user] load: search and pages', () => {
  test.each(boardCases)('$name', async ({ search, want, requests }) => {
    arrange({});
    const { page } = await loadPage(search);
    expect({ view: viewOf(page!), requests: pageRequests }).toEqual({ view: want, requests });
  });

  test('the podium is the same overall top with or without a search or page', async () => {
    arrange({});
    const plain = (await loadPage()).page!.podium;
    const searched = (await loadPage('?q=v5&page=2')).page!.podium;
    expect(searched).toEqual(plain);
  });

  test('a failing standings read stays degraded with an empty board for a search', async () => {
    arrange({ loyaltyFails: true });
    const { page } = await loadPage('?q=v1&page=3');
    expect({ degraded: page!.degraded, rows: page!.rows.length, query: page!.query }).toEqual({ degraded: true, rows: 0, query: 'v1' });
  });
});

describe('(public)/[user] load: edge cache tag', () => {
  test('a rendered board is tagged with its channel', async () => {
    arrange({});
    await loadPage('?page=2');
    expect(lastLocals.edgeCacheTag).toBe('lb:somechannel');
  });

  test('a hidden board is tagged too so a later purge reaches its cached 404', async () => {
    arrange({ leaderboardPage: async () => false });
    await loadPage('?q=zz');
    expect(lastLocals.edgeCacheTag).toBe('lb:somechannel');
  });
});

describe('boardHref', () => {
  test.each([
    { name: 'page 1 without a query is the bare path', params: { query: '', page: 1 }, want: '/demochan' },
    { name: 'a later page keeps only the page', params: { query: '', page: 3 }, want: '/demochan?page=3' },
    { name: 'a query on page 1 keeps only the query', params: { query: 'ab c', page: 1 }, want: '/demochan?q=ab+c' },
    { name: 'a query on a later page keeps both', params: { query: 'x', page: 2 }, want: '/demochan?q=x&page=2' }
  ])('$name', ({ params, want }) => {
    expect(boardHref(params, '/demochan')).toBe(want);
  });
});

type Small = {
  viewers: number;
  podium: number;
  rows: number[];
  range: Range;
  pages: number;
  copy: string | null;
};

const smallBoards: { name: string; want: Small }[] = [
  { name: 'no viewers', want: { viewers: 0, podium: 0, rows: [], range: { from: 0, to: 0, total: 0 }, pages: 1, copy: 'leaderboard.emptyBody' } },
  { name: 'one viewer', want: { viewers: 1, podium: 1, rows: [], range: { from: 0, to: 0, total: 1 }, pages: 1, copy: 'leaderboard.soloNote' } },
  { name: 'two viewers', want: { viewers: 2, podium: 2, rows: [], range: { from: 0, to: 0, total: 2 }, pages: 1, copy: 'leaderboard.podiumOnlyNote' } },
  { name: 'three viewers', want: { viewers: 3, podium: 3, rows: [], range: { from: 0, to: 0, total: 3 }, pages: 1, copy: 'leaderboard.podiumOnlyNote' } },
  { name: 'four viewers', want: { viewers: 4, podium: 3, rows: [4], range: { from: 4, to: 4, total: 4 }, pages: 1, copy: null } }
];

describe('(public)/[user] load: boards of 0 to 4 viewers', () => {
  test.each(smallBoards)('$name', async ({ want }) => {
    arrange({ viewers: want.viewers });
    const data = (await loadPage()).page!;
    const copy = data.rows.length ? null : emptyCopyKeys({ query: data.query, degraded: data.degraded, podiumSize: data.podium.length })?.body ?? null;
    expect({
      viewers: want.viewers,
      podium: data.podium.length,
      rows: data.rows.map((row) => row.rank),
      range: data.range,
      pages: data.pages,
      copy
    }).toEqual(want);
  });

  test('a degraded board shows no empty copy even with a query', async () => {
    arrange({ loyaltyFails: true });
    const data = (await loadPage('?q=v1')).page!;
    expect(emptyCopyKeys({ query: data.query, degraded: data.degraded, podiumSize: data.podium.length })).toBeNull();
  });
});

describe('(public)/[user] load: search past the last page', () => {
  test('a search page past the end falls back to the last search page', async () => {
    arrange({});
    const { page } = await loadPage('?q=v&page=99');
    expect({ view: viewOf(page!), requests: pageRequests }).toEqual({
      view: { first: 76, count: 12, query: 'v', page: 6, pages: 6, range: { from: 76, to: 87, total: 87 } },
      requests: [
        { limit: 15, offset: 1470, query: 'v' },
        { limit: 15, offset: 75, query: 'v' }
      ]
    });
  });

  test('a search with fewer matches than the requested page falls back to page 1', async () => {
    arrange({});
    const { page } = await loadPage('?q=v1&page=9');
    expect({ view: viewOf(page!), requests: pageRequests }).toEqual({
      view: { first: 1, count: 11, query: 'v1', page: 1, pages: 1, range: { from: 1, to: 11, total: 11 } },
      requests: [
        { limit: 15, offset: 120, query: 'v1' },
        { limit: 15, offset: 0, query: 'v1' }
      ]
    });
  });
});
