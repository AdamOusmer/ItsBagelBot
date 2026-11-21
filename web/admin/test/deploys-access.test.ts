// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { allows, type AccessKey, type AdminRole } from '../src/lib/access';
import type { DeployPlan, FluxTier } from '../src/lib/deploys/types';
import type { DeployRuns } from '../src/lib/server/release';
import { stubSvelteKit } from './sveltekit';

stubSvelteKit();

type Session = { user_id: string } | null;

const session = { user_id: '1' };
const params = { id: 'run-1' };
const admin = { id: session.user_id, login: 'mod', display_name: 'Mod' };
const plan = { in_sync: true } satisfies DeployPlan;
const pinnable = ['users'];

let role: AdminRole = 'admin';

function adminFor(session: Session) {
  return session ? { ...admin, role } : null;
}

mock.module('#lib/server/access', () => ({
  requireAdmin: async (session: Session) => adminFor(session),
  allows,
  requireRole: async ({ locals }: { locals: { session: Session } }, key: AccessKey) =>
    allows(role, key) ? adminFor(locals.session) : null
}));

const loadDeployRun = mock(async () => ({ run: {} }));
const listRuns = mock(async (): Promise<DeployRuns> => ({ runs: [], activeRunId: null }));
const getRun = mock(async () => params);
const readPlan = mock(async (): Promise<DeployPlan | null> => plan);
const fluxTiers = mock(async (): Promise<FluxTier[]> => []);
const deployApi = mock(async () => ({ list: listRuns, get: getRun, plan: readPlan }));

mock.module('#lib/server/deploys', () => ({
  deployActions: {},
  loadDeployRun,
  runActions: {},
  deployApi,
  deployStatus: () => 503,
  RunRelay: class {}
}));
mock.module('#lib/server/flux', () => ({ fluxTiers, PINNABLE: pinnable }));

const { load: indexLoad } = await import('../src/routes/(admin)/deploys/+page.server');
const { load: newLoad } = await import('../src/routes/(admin)/deploys/new/+page.server');
const { load: runLoad } = await import('../src/routes/(admin)/deploys/[id]/+page.server');
const { GET: streamGET } = await import('../src/routes/(admin)/deploys/[id]/stream/+server');

beforeEach(() => {
  role = 'admin';
  for (const read of [deployApi, loadDeployRun, listRuns, getRun, readPlan, fluxTiers]) read.mockClear();
});

function openIndex() {
  return indexLoad({ parent: async () => ({ id: admin.id, role }) } as Parameters<typeof indexLoad>[0]);
}

function openNew(session: Session) {
  return newLoad({ locals: { session } } as Parameters<typeof newLoad>[0]);
}

type RouteCase = {
  label: string;
  session: Session;
  call: (session: Session) => unknown;
  refusal: { status: number; location?: string };
  untouched: () => unknown;
};

const routes = [
  { name: 'new deploy page', call: openNew, loader: deployApi, kind: 'page' },
  {
    name: 'deploy run page',
    call: (session: Session) => runLoad({ locals: { session }, params } as Parameters<typeof runLoad>[0]),
    loader: loadDeployRun,
    kind: 'page'
  },
  {
    name: 'deploy run stream',
    call: (session: Session) => streamGET({ locals: { session }, params } as Parameters<typeof streamGET>[0]),
    loader: deployApi,
    kind: 'stream'
  }
] as const;

function refusalFor(kind: 'page' | 'stream', session: Session) {
  if (kind === 'stream') return { status: 403 };
  return { status: 302, location: session ? '/' : '/login' };
}

const cases: RouteCase[] = routes.flatMap(({ name, call, loader, kind }) =>
  [session, null].map((requestSession) => ({
    label: `${name} refuses ${requestSession ? 'an admin without deploys.manage' : 'an anonymous session'}`,
    session: requestSession,
    call,
    refusal: refusalFor(kind, requestSession),
    untouched: loader
  }))
);

describe('deploy routes require deploys.manage', () => {
  test.each(cases)('$label', async ({ session, call, refusal, untouched }: RouteCase) => {
    await expect(call(session)).rejects.toMatchObject(refusal);
    expect(untouched).not.toHaveBeenCalled();
  });

  test('refuses an admin on the deploy index before starting reads', async () => {
    await expect(openIndex()).rejects.toMatchObject({ status: 302, location: '/' });
    expect(deployApi).not.toHaveBeenCalled();
    expect(fluxTiers).not.toHaveBeenCalled();
  });
});

describe('new deploy page', () => {
  beforeEach(() => {
    role = 'owner';
  });

  test('returns the pending plan in page data', async () => {
    const pending = Promise.withResolvers<DeployPlan | null>();
    readPlan.mockImplementationOnce(() => pending.promise);

    const data = await openNew(session);

    expect({ planned: data?.planned instanceof Promise, planReads: readPlan.mock.calls }).toEqual({
      planned: true,
      planReads: [[{ actor_id: admin.id }]]
    });
    pending.resolve(plan);
    expect(await data?.planned).toEqual({ plan, planError: null });
  });

  test('redirects to an active run', async () => {
    listRuns.mockResolvedValueOnce({ runs: [], activeRunId: params.id });
    await expect(openNew(session)).rejects.toMatchObject({ status: 303, location: '/deploys/run-1' });
    expect(getRun).toHaveBeenCalledWith({ actor_id: admin.id, run_id: params.id });
  });
});

test('the deploy index streams the plan and Flux while history is pending', async () => {
  role = 'owner';
  const pending = Promise.withResolvers<DeployRuns>();
  listRuns.mockImplementationOnce(() => pending.promise);

  const data = await openIndex();

  expect([data?.runs, data?.planned, data?.flux].every((read) => read instanceof Promise)).toBe(true);
  expect(await Promise.all([data?.planned, data?.flux])).toEqual([
    { plan, planError: null },
    { tiers: [], pinnable, fluxError: null }
  ]);
  pending.resolve({ runs: [], activeRunId: null });
  expect(await data?.runs).toEqual({ runs: [], active: null, runsError: null });
});
