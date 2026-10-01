// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';

class TestRedirect extends Error {
  constructor(
    readonly status: number,
    readonly location: string
  ) {
    super(`Redirect to ${location}`);
  }
}

class TestHttpError extends Error {
  constructor(readonly status: number, body: string) {
    super(body);
  }
}

type Admin = { id: string; login: string; display_name: string; role: 'moderator' | 'admin' | 'owner' };

let adminReply: Admin | null = { id: '1', login: 'mod', display_name: 'Mod', role: 'admin' };
let allowsReply = false;

const requireAdmin = mock(async () => adminReply);
const allows = mock(() => allowsReply);
const requireRole = mock(async () => (allowsReply ? adminReply : null));

mock.module('$lib/server/access', () => ({ requireAdmin, allows, requireRole }));
mock.module('@sveltejs/kit', () => ({
  redirect: (status: number, location: string) => new TestRedirect(status, location),
  error: (status: number, body: string) => new TestHttpError(status, body)
}));

const loadDeploys = mock(async () => ({ planned: Promise.resolve({ plan: null, planError: null }), runs: [], active: null, runsError: null }));
const loadDeployRun = mock(async () => ({ run: {} }));
const deployApi = mock(async () => ({ get: mock(async () => ({})), watch: mock(() => () => {}) }));
const deployStatus = () => 503;
class RunRelay {
  offer() {}
  open() {}
  keepalive() {}
}

mock.module('$lib/server/deploys', () => ({
  loadDeploys,
  loadDeployRun,
  runActions: {},
  deployApi,
  deployStatus,
  RunRelay
}));

const { load: newLoad } = await import('../src/routes/(admin)/deploys/new/+page.server');
const { load: runLoad } = await import('../src/routes/(admin)/deploys/[id]/+page.server');
const { GET: streamGET } = await import('../src/routes/(admin)/deploys/[id]/stream/+server');

beforeEach(() => {
  adminReply = { id: '1', login: 'mod', display_name: 'Mod', role: 'admin' };
  allowsReply = false;
  requireAdmin.mockClear();
  allows.mockClear();
  requireRole.mockClear();
  loadDeploys.mockClear();
  loadDeployRun.mockClear();
  deployApi.mockClear();
});

type Session = { user_id: string } | null;

type RouteCase = {
  label: string;
  admin: boolean;
  call: (session: Session) => unknown;
  refusal: Record<string, unknown>;
  untouched: ReturnType<typeof mock>;
};

const params = { id: 'run-1' };

const routes = [
  { name: 'new deploy page', call: (session: Session) => newLoad({ locals: { session } } as never), loader: loadDeploys, kind: 'page' },
  { name: 'deploy run page', call: (session: Session) => runLoad({ locals: { session }, params } as never), loader: loadDeployRun, kind: 'page' },
  { name: 'deploy run stream', call: (session: Session) => streamGET({ locals: { session }, params } as never), loader: deployApi, kind: 'stream' }
] as const;

function refusalFor(kind: 'page' | 'stream', admin: boolean): Record<string, unknown> {
  if (kind === 'stream') return { status: 403 };
  return { status: 302, location: admin ? '/' : '/login' };
}

const cases: RouteCase[] = routes.flatMap((route) =>
  [true, false].map((admin) => ({
    label: `${route.name} refuses ${admin ? 'an admin without deploys.manage' : 'a session with no admin'}`,
    admin,
    call: route.call,
    refusal: refusalFor(route.kind, admin),
    untouched: route.loader
  }))
);

async function outcome(run: () => unknown): Promise<unknown> {
  try {
    await run();
    return null;
  } catch (e) {
    return e;
  }
}

describe('deploy routes require deploys.manage', () => {
  test.each(cases)('$label', async ({ admin, call, refusal, untouched }: RouteCase) => {
    if (!admin) adminReply = null;
    const refused = await outcome(() => call(admin ? { user_id: '1' } : null));
    expect(refused).toMatchObject(refusal);
    expect(untouched).not.toHaveBeenCalled();
  });
});
