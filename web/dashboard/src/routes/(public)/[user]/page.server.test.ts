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
  busy: boolean;
  commandsPage: boolean;
};

type PageRequest = { limit: number; offset: number; viewerId?: string; viewerLogin?: string };
type Resolution = { kind: 'found'; viewerId: string } | { kind: 'missing' } | { kind: 'busy' } | { kind: 'error' };

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
let resolution: Resolution = { kind: 'missing' };
const resolverCalls: string[] = [];
const reconciled = new Map<string, string>();

function activeBoard(): Row[] {
  return board.slice(0, viewerCount ?? board.length).map((row) => ({ ...row, viewerLogin: reconciled.get(row.viewerId) ?? row.viewerLogin }));
}

function standingsHits(req: PageRequest): Row[] {
  const rows = activeBoard();
  if (req.viewerId) return rows.filter((row) => row.viewerId === req.viewerId);
  if (req.viewerLogin) return rows.filter((row) => row.viewerLogin === req.viewerLogin);
  return rows;
}

function reconcile(req: PageRequest): void {
  if (!req.viewerId || !req.viewerLogin) return;
  if (standingsHits(req).length > 0) reconciled.set(req.viewerId, req.viewerLogin);
}

function serveStandings(req: PageRequest): { rows: Row[]; total: number } {
  pageRequests.push(req);
  reconcile(req);
  const hits = standingsHits(req);
  const searched = Boolean(req.viewerId || req.viewerLogin);
  return { rows: searched ? hits : hits.slice(req.offset, req.offset + req.limit), total: hits.length };
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
    return serveStandings(req);
  }
}));
mock.module('$lib/server/viewer-lookup', () => ({
  resolveCurrentViewer: async (login: string) => {
    resolverCalls.push(login);
    return resolution;
  }
}));
mock.module('$lib/server/services', () => ({
  resolveLogin: async (login: string) => ({ userId: '42', username: login }),
  userCommandsPage: (id: string) => commandsPageReply(id),
  userLeaderboardPage: (id: string) => leaderboardPageReply(id)
}));

const { load } = await import('./+page.server');

type Outcome = { status?: number; message?: string; location?: string; edge?: unknown; page?: Page };

let lastLocals: Record<string, unknown> = {};

async function loadPage(search = '', user = 'somechannel'): Promise<Outcome> {
  const locals: Record<string, unknown> = { locale: 'en' };
  lastLocals = locals;
  const event = {
    params: { user },
    url: new URL(`https://leaderboard.itsbagelbot.com/${user}${search}`),
    locals
  };
  try {
    return { page: (await load(event as never)) as unknown as Page, edge: locals.edgeOutcome };
  } catch (e) {
    const err = e as { status?: number; location?: string; body?: { message?: string } };
    return { status: err.status, message: err.body?.message, location: err.location, edge: locals.edgeOutcome };
  }
}

type Setup = {
  commandsPage?: () => Promise<boolean>;
  leaderboardPage?: () => Promise<boolean>;
  loyaltyFails?: boolean;
  loyaltyEnabled?: boolean;
  viewers?: number;
  resolution?: Resolution;
};

function arrange(setup: Setup): void {
  pageRequests.length = 0;
  resolverCalls.length = 0;
  reconciled.clear();
  resolution = setup.resolution ?? { kind: 'missing' };
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

const notFound = { status: 404, message: 'Channel not found', edge: 'hidden' };

describe('(public)/[user] load: leaderboard visibility', () => {
  test('a hidden leaderboard 404s edge-cached with the unknown-channel message and no board data', async () => {
    arrange({ leaderboardPage: async () => false });
    expect(await loadPage()).toEqual(notFound);
  });

  test('a disabled loyalty module 404s edge-cached', async () => {
    arrange({ loyaltyEnabled: false });
    expect(await loadPage()).toEqual(notFound);
  });

  test('a rejected leaderboard-flag read fails open and renders', async () => {
    arrange({
      leaderboardPage: async () => {
        throw new Error('users service blip');
      }
    });
    const out = await loadPage();
    expect({ status: out.status, edge: out.edge, top: out.page?.podium.length }).toEqual({
      status: undefined,
      edge: 'skip',
      top: 1
    });
  });

  test('a rejected loyalty read renders the degraded page instead of a 404', async () => {
    arrange({ loyaltyFails: true });
    const out = await loadPage();
    expect({ status: out.status, edge: out.edge, degraded: out.page?.degraded }).toEqual({
      status: undefined,
      edge: 'skip',
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
    requests: [{ limit: 15, offset: 3 }]
  },
  {
    name: 'page 2 starts at rank 19',
    search: '?page=2',
    want: { first: 19, count: 15, query: '', page: 2, pages: 6, range: { from: 19, to: 33, total: 87 } },
    requests: [{ limit: 15, offset: 18 }]
  },
  {
    name: 'a page past the end falls back to the last page',
    search: '?page=99',
    want: { first: 79, count: 9, query: '', page: 6, pages: 6, range: { from: 79, to: 87, total: 87 } },
    requests: [
      { limit: 15, offset: 1473 },
      { limit: 15, offset: 78 }
    ]
  },
  {
    name: 'a numeric id search returns that viewer with the real board rank, top three included',
    search: '?q=2',
    want: { first: 2, count: 1, query: '2', page: 1, pages: 1, range: { from: 1, to: 1, total: 1 } },
    requests: [{ limit: 15, offset: 0, viewerId: '2' }]
  },
  {
    name: 'a stored login returns that viewer with the real board rank',
    search: '?q=v5',
    want: { first: 5, count: 1, query: 'v5', page: 1, pages: 1, range: { from: 1, to: 1, total: 1 } },
    requests: [{ limit: 15, offset: 0, viewerLogin: 'v5' }]
  },
  {
    name: 'an id with no balance in this channel reports an empty single page',
    search: '?q=9999',
    want: { first: undefined, count: 0, query: '9999', page: 1, pages: 1, range: { from: 0, to: 0, total: 0 } },
    requests: [{ limit: 15, offset: 0, viewerId: '9999' }]
  },
  {
    name: 'a login with no balance and no Twitch match reports an empty single page',
    search: '?q=ghost',
    want: { first: undefined, count: 0, query: 'ghost', page: 1, pages: 1, range: { from: 0, to: 0, total: 0 } },
    requests: [{ limit: 15, offset: 0, viewerLogin: 'ghost' }]
  },
  {
    name: 'an id at the 20 digit cap is searched as is',
    search: `?q=${'9'.repeat(20)}`,
    want: { first: undefined, count: 0, query: '9'.repeat(20), page: 1, pages: 1, range: { from: 0, to: 0, total: 0 } },
    requests: [{ limit: 15, offset: 0, viewerId: '9'.repeat(20) }]
  },
  ...['a-b', 'ab%25', 'caf%C3%A9', '%E2%84%AAim', '1%202', '9'.repeat(21), 'a'.repeat(26)].map((raw) => {
    const query = decodeURIComponent(raw);
    return {
      name: `invalid text "${query}" renders the no-match state without redirecting or searching`,
      search: `?q=${raw}`,
      want: { first: undefined, count: 0, query, page: 1, pages: 1, range: { from: 0, to: 0, total: 0 } },
      requests: []
    };
  })
];

const redirectCases: { name: string; search: string; want: string }[] = [
  { name: 'a padded id trims', search: '?q=+42+', want: '/somechannel?q=42' },
  { name: 'leading zeros are stripped', search: '?q=007', want: '/somechannel?q=7' },
  { name: 'an all-zero id drops the param', search: '?q=000', want: '/somechannel' },
  { name: 'an uppercase login lowercases', search: '?q=V8', want: '/somechannel?q=v8' },
  { name: 'an at-prefixed mixed-case login loses the at sign and lowercases', search: '?q=%40CrumbCatcher', want: '/somechannel?q=crumbcatcher' },
  { name: 'leading at signs and spaces are stripped', search: '?q=%20%40%40Otter%20', want: '/somechannel?q=otter' },
  { name: 'a digit-led login stays a login', search: '?q=12AB', want: '/somechannel?q=12ab' },
  { name: 'padding around invalid text trims without lowercasing', search: '?q=+Caf%C3%A9+', want: '/somechannel?q=Caf%C3%A9' },
  { name: 'invalid text over 26 characters is cut to 26', search: `?q=${'a'.repeat(30)}`, want: `/somechannel?q=${'a'.repeat(26)}` },
  { name: 'an id over 21 digits is cut to 21', search: `?q=${'9'.repeat(30)}`, want: `/somechannel?q=${'9'.repeat(21)}` },
  { name: 'a 25 character login is kept', search: `?q=${'A'.repeat(25)}`, want: `/somechannel?q=${'a'.repeat(25)}` },
  { name: 'an empty search drops the param', search: '?q=', want: '/somechannel' },
  { name: 'page 1 drops the param', search: '?page=1', want: '/somechannel' },
  { name: 'a zero-padded page normalizes', search: '?page=02', want: '/somechannel?page=2' },
  { name: 'a malformed page drops the param', search: '?page=abc', want: '/somechannel' },
  { name: 'a negative page drops the param', search: '?page=-3', want: '/somechannel' },
  { name: 'a page past the cap clamps to the cap', search: '?page=999', want: '/somechannel?page=399' },
  { name: 'other params survive', search: '?q=42+&lang=fr&page=1', want: '/somechannel?lang=fr&q=42' }
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

  test('a redirect never reaches the resolver', async () => {
    arrange({});
    await loadPage('?q=CrumbCatcher');
    expect(resolverCalls).toEqual([]);
  });

  test('invalid search text never reaches the resolver', async () => {
    arrange({});
    await loadPage('?q=caf%C3%A9');
    expect(resolverCalls).toEqual([]);
  });

  test('a degraded board is flagged uncacheable', async () => {
    arrange({ loyaltyFails: true });
    await loadPage('?q=1');
    expect(lastLocals.edgeOutcome).toBe('skip');
  });

  test('a healthy board is not flagged', async () => {
    arrange({});
    await loadPage('?q=1');
    expect(lastLocals.edgeOutcome).toBe('page');
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
    const searched = (await loadPage('?q=5&page=2')).page!.podium;
    expect(searched).toEqual(plain);
  });

  test('a failing standings read stays degraded with an empty board for a search', async () => {
    arrange({ loyaltyFails: true });
    const { page } = await loadPage('?q=1&page=3');
    expect({ degraded: page!.degraded, rows: page!.rows.length, query: page!.query }).toEqual({ degraded: true, rows: 0, query: '1' });
  });
});

describe('(public)/[user] load: username search and reconcile', () => {
  test('an id search never calls the resolver', async () => {
    arrange({});
    await loadPage('?q=2');
    expect(resolverCalls).toEqual([]);
  });

  test('a stored login hit never calls the resolver or reconciles', async () => {
    arrange({ resolution: { kind: 'found', viewerId: '9' } });
    const { page } = await loadPage('?q=v5');
    expect({ rank: page!.rows[0]?.rank, calls: resolverCalls, requests: pageRequests, busy: page!.busy }).toEqual({
      rank: 5,
      calls: [],
      requests: [{ limit: 15, offset: 0, viewerLogin: 'v5' }],
      busy: false
    });
    expect(lastLocals.edgeOutcome).toBe('page');
  });

  test('a stored miss resolves the current id then looks it up with the login as the reconcile hint', async () => {
    arrange({ resolution: { kind: 'found', viewerId: '7' } });
    const { page } = await loadPage('?q=renamed_seven');
    expect({ rows: page!.rows.map((row) => [row.rank, row.viewerLogin]), calls: resolverCalls, requests: pageRequests }).toEqual({
      rows: [[7, 'renamed_seven']],
      calls: ['renamed_seven'],
      requests: [
        { limit: 15, offset: 0, viewerLogin: 'renamed_seven' },
        { limit: 15, offset: 0, viewerId: '7', viewerLogin: 'renamed_seven' }
      ]
    });
    expect(lastLocals.edgeOutcome).toBe('page');
  });

  test('after the reconcile the new login is a stored hit', async () => {
    arrange({ resolution: { kind: 'found', viewerId: '7' } });
    await loadPage('?q=renamed_seven');
    pageRequests.length = 0;
    resolverCalls.length = 0;
    const { page } = await loadPage('?q=renamed_seven');
    expect({ rank: page!.rows[0]?.rank, calls: resolverCalls, requests: pageRequests.length }).toEqual({ rank: 7, calls: [], requests: 1 });
  });

  test('a resolved id with no balance in this channel is an empty result', async () => {
    arrange({ resolution: { kind: 'found', viewerId: '99999' } });
    const { page } = await loadPage('?q=stranger');
    expect({ rows: page!.rows.length, calls: resolverCalls, busy: page!.busy }).toEqual({ rows: 0, calls: ['stranger'], busy: false });
  });

  test('an unknown name is an empty result after one resolver call', async () => {
    arrange({ resolution: { kind: 'missing' } });
    const { page } = await loadPage('?q=nobody_here');
    expect({ rows: page!.rows.length, degraded: page!.degraded, busy: page!.busy, calls: resolverCalls, requests: pageRequests.length }).toEqual({
      rows: 0,
      degraded: false,
      busy: false,
      calls: ['nobody_here'],
      requests: 1
    });
    expect(lastLocals.edgeOutcome).toBe('page');
  });

  test('a resolver error is a no-match, not a degraded page or an error', async () => {
    arrange({ resolution: { kind: 'error' } });
    const out = await loadPage('?q=nobody_here');
    expect({ status: out.status, rows: out.page?.rows.length, degraded: out.page?.degraded, busy: out.page?.busy }).toEqual({
      status: undefined,
      rows: 0,
      degraded: false,
      busy: false
    });
  });

  test('an empty bucket shows the busy note and skips the edge cache', async () => {
    arrange({ resolution: { kind: 'busy' } });
    const { page } = await loadPage('?q=nobody_here');
    expect({
      rows: page!.rows.length,
      busy: page!.busy,
      copy: emptyCopyKeys({ query: page!.query, degraded: page!.degraded, busy: page!.busy, podiumSize: page!.podium.length })?.body
    }).toEqual({ rows: 0, busy: true, copy: 'searchBusyBody' });
    expect(lastLocals.edgeOutcome).toBe('skip');
  });
});

describe('(public)/[user] load: edge cache tag', () => {
  test('a rendered board or search is tagged with its channel', async () => {
    arrange({});
    await loadPage('?q=fan&page=2');
    expect(lastLocals.edgeCacheTag).toBe('lb:somechannel');
  });

  test('a hidden board is tagged too so a later purge reaches its cached 404', async () => {
    arrange({ leaderboardPage: async () => false });
    await loadPage('?q=22');
    expect({ tag: lastLocals.edgeCacheTag, edge: lastLocals.edgeOutcome }).toEqual({ tag: 'lb:somechannel', edge: 'hidden' });
  });
});

describe('(public)/[user] load: one URL per spelling', () => {
  test.each(['somechannel', '@somechannel'])('%s renders in place', async (user) => {
    arrange({});
    expect((await loadPage('?page=2', user)).edge).toBe('page');
  });

  test.each([
    { user: 'SomeChannel', search: '', want: '/somechannel' },
    { user: '@@somechannel', search: '?page=2', want: '/somechannel?page=2' },
    { user: 'SomeChannel', search: '?page=1&q=', want: '/somechannel' },
    { user: '@somechannel', search: '?page=01', want: '/@somechannel' }
  ])('$user$search redirects once to $want', async ({ user, search, want }) => {
    arrange({});
    expect(await loadPage(search, user)).toEqual({ status: 308, location: want, edge: undefined });
  });

  test('an unknown login 404s as a short-lived miss', async () => {
    arrange({});
    expect((await loadPage('', 'bad.login')).edge).toBe('missing');
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
  { name: 'no viewers', want: { viewers: 0, podium: 0, rows: [], range: { from: 0, to: 0, total: 0 }, pages: 1, copy: 'emptyBody' } },
  { name: 'one viewer', want: { viewers: 1, podium: 1, rows: [], range: { from: 0, to: 0, total: 1 }, pages: 1, copy: 'soloNote' } },
  { name: 'two viewers', want: { viewers: 2, podium: 2, rows: [], range: { from: 0, to: 0, total: 2 }, pages: 1, copy: 'podiumOnlyNote' } },
  { name: 'three viewers', want: { viewers: 3, podium: 3, rows: [], range: { from: 0, to: 0, total: 3 }, pages: 1, copy: 'podiumOnlyNote' } },
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
    const data = (await loadPage('?q=1')).page!;
    expect(emptyCopyKeys({ query: data.query, degraded: data.degraded, podiumSize: data.podium.length })).toBeNull();
  });
});

describe('(public)/[user] load: search past the last page', () => {
  test('a search with fewer matches than the requested page falls back to page 1', async () => {
    arrange({});
    const { page } = await loadPage('?q=1&page=9');
    expect({ view: viewOf(page!), requests: pageRequests }).toEqual({
      view: { first: 1, count: 1, query: '1', page: 1, pages: 1, range: { from: 1, to: 1, total: 1 } },
      requests: [
        { limit: 15, offset: 120, viewerId: '1' },
        { limit: 15, offset: 0, viewerId: '1' }
      ]
    });
  });
});
