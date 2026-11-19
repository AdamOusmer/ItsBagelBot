// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/env';
import { MOD } from '@bagel/kit';
import { env } from '@bagel/kit/server/env';
import { error, fail, redirect } from '@sveltejs/kit';
import { NEW_FLOW_ID } from '#lib/flow/ref';
import type { ChoiceList } from '#lib/flow/choices';
import { effectiveId } from '#lib/server/board';
import { lookupTwitchUser } from '#lib/server/identity-heal';
import { counterChoices, manageableRewardChoices, namedCooldownChoices, namedCountdownChoices, recipeChoices, rewardChoices, sceneChoices } from '#lib/server/flow-choices';
import { runAutomation } from '#lib/server/automations-actions';
import { listRuns } from '#lib/server/automations-store';
import { parseFlowRouteId, type FlowRef } from '#lib/flow/ref';
import { signedInAction } from '#lib/server/action-context';
import { actionError, actionErrorBody } from '#lib/server/action-errors';
import { rehearseFetchDef } from '#lib/server/fetch-def-actions';
import { parseRewardDraft } from '#lib/server/flow-reward';
import { flowDenial, liveEditor, liveRewardSave, liveSave, lowerSaveRequest, newEditor, type EditorData } from '#lib/server/flow-page';
import { goveeStore, type GoveeDevice } from '#lib/server/govee-store';
import { broadcasterPremium, gateModulePage } from '#lib/server/module-gate';

const DEMO = dev && env.DEMO === '1';
const recipeAction = signedInAction((session) => gateModulePage(session, MOD.automations), DEMO);
const RUN_REFUSAL_STATUS = new Map([['invalid', 400], ['forbidden', 403], ['not_found', 404]]);

export type GoveeLights = { keyPresent: boolean; devices: GoveeDevice[]; code?: 'key_rejected' | 'unreachable' };

async function goveeLights(locals: App.Locals, url: URL): Promise<GoveeLights> {
  if (DEMO) {
    const { demoGoveeDevices } = await import('#lib/server/demo-data');
    return { keyPresent: url.searchParams.get('setup') !== '1', devices: demoGoveeDevices() };
  }
  const store = goveeStore(effectiveId(locals.session));
  const { keyPresent } = await store.read();
  if (!keyPresent) return { keyPresent, devices: [] };
  const { devices, code } = await store.listDevices();
  return { keyPresent, devices, code };
}

async function editorFor(locals: App.Locals, routeId: string): Promise<EditorData | null> {
  if (!DEMO) return liveEditor(locals, routeId);
  return routeId === NEW_FLOW_ID ? newEditor() : (await import('#lib/flow/demo')).demoEditor(routeId);
}

type Choices = {
  rewards: Promise<ChoiceList>;
  manageableRewards: Promise<ChoiceList>;
  counters: Promise<ChoiceList>;
  namedCooldowns: Promise<ChoiceList>;
  namedCountdowns: Promise<ChoiceList>;
  recipes: Promise<ChoiceList>;
};

async function choicesFor(locals: App.Locals, url: URL, ref: FlowRef | null): Promise<Choices> {
  const current = ref?.layer === 'automation' ? ref.id : undefined;
  if (DEMO) {
    const demo = await import('#lib/flow/demo');
    const lists = demo.demoChoices(demo.demoChoiceMode(url.searchParams.get('choices')), current);
    return {
      rewards: Promise.resolve(lists.rewards),
      manageableRewards: Promise.resolve(lists.rewards),
      counters: Promise.resolve(lists.counters),
      namedCooldowns: Promise.resolve(lists.namedCooldowns),
      namedCountdowns: Promise.resolve(lists.namedCountdowns),
      recipes: Promise.resolve(lists.recipes)
    };
  }
  const uid = effectiveId(locals.session);
  return { rewards: rewardChoices(uid), manageableRewards: manageableRewardChoices(uid), counters: counterChoices(uid), namedCooldowns: namedCooldownChoices(uid), namedCountdowns: namedCountdownChoices(uid), recipes: recipeChoices(uid, current) };
}

export const load: PageServerLoad = async ({ params, locals, url, depends }) => {
  if (!(await broadcasterPremium(locals))) throw redirect(303, '/flows');
  depends('flows:govee', 'flows:choices');
  const editor = await editorFor(locals, params.id);
  if (!editor) throw error(404);
  const govee = goveeLights(locals, url).catch((): GoveeLights => ({ keyPresent: true, devices: [], code: 'unreachable' }));
  return { ...editor, govee, ...(await choicesFor(locals, url, editor.ref)) };
};

const LOGIN_PATTERN = /^[a-z0-9_]{1,25}$/;
const DEMO_MISSING_LOGIN = 'nosuchuser';
const DEVICE_PATTERN = /^[A-Za-z0-9:_-]{1,64}$/;

async function scenesFor(locals: App.Locals, device: string, sku: string): Promise<ChoiceList> {
  if (DEMO) return (await import('#lib/flow/demo')).demoScenes(device);
  return sceneChoices(effectiveId(locals.session), { device, sku });
}

function demoResolve(login: string) {
  if (login === DEMO_MISSING_LOGIN) return fail(404, { ok: false, code: 'missing' });
  return { ok: true, userId: '100200300', login };
}

async function twitchResolve(login: string) {
  const found = await lookupTwitchUser(login);
  if (found.kind === 'missing') return fail(404, { ok: false, code: 'missing' });
  if (found.kind !== 'found') return fail(502, { ok: false, code: 'failed' });
  return { ok: true, userId: found.value.id, login: found.value.login };
}

async function runsFor(locals: App.Locals, id: string) {
  if (DEMO) return (await import('#lib/flow/demo')).demoRuns(id);
  return listRuns(effectiveId(locals.session), id);
}

async function runSavedRecipe(locals: App.Locals, form: FormData) {
  const result = await runAutomation(effectiveId(locals.session), form, locals).catch(() => null);
  if (typeof result === 'string') return { ok: true };
  const code = result && 'code' in result.data ? String(result.data.code ?? 'failed') : 'failed';
  return fail(RUN_REFUSAL_STATUS.get(code) ?? 502, { ok: false, code });
}

export const actions: Actions = {
  runs: async ({ request, locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false });
    const ref = parseFlowRouteId(String((await request.formData()).get('id') ?? ''));
    if (ref?.layer !== 'automation') return fail(400, { ok: false });
    try {
      return { ok: true, runs: await runsFor(locals, ref.id) };
    } catch {
      return fail(502, { ok: false });
    }
  },
  save: async ({ request, locals }) => {
    const denied = await flowDenial(locals);
    if (denied) return denied;
    const lowered = lowerSaveRequest(String((await request.formData()).get('payload') ?? ''));
    if (!lowered.ok) return lowered.reply;
    return DEMO ? (await import('#lib/flow/demo')).demoSave(lowered.lowered) : liveSave(locals, lowered.lowered);
  },
  run: async ({ request, locals }) => {
    const denied = await flowDenial(locals);
    if (denied) return fail(403, { ok: false, code: 'forbidden', error: denied.error });
    const form = await request.formData();
    const name = form.get('name');
    if (typeof name !== 'string' || !name.trim()) return fail(400, { ok: false, code: 'invalid' });
    return DEMO ? (await import('#lib/flow/demo')).demoRun() : runSavedRecipe(locals, form);
  },
  reward: async ({ request, locals }) => {
    const denied = await flowDenial(locals);
    if (denied) return denied;
    const draft = parseRewardDraft(String((await request.formData()).get('payload') ?? ''));
    if (!draft) return fail(400, { ok: false, error: actionError(locals.locale, 'Invalid reward.') });
    const reply = DEMO ? (await import('#lib/flow/demo')).demoRewardSave(locals.locale, draft) : await liveRewardSave(locals, draft);
    return reply.ok ? reply : fail(reply.status, reply);
  },
  scenes: async ({ request, locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false });
    const form = await request.formData();
    const device = String(form.get('device') ?? '');
    const sku = String(form.get('sku') ?? '');
    if (!DEVICE_PATTERN.test(device) || !DEVICE_PATTERN.test(sku)) return fail(400, { ok: false });
    return { ok: true, list: await scenesFor(locals, device, sku) };
  },
  resolveUser: async ({ request, locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false, code: 'failed' });
    const login = String((await request.formData()).get('login') ?? '');
    if (!LOGIN_PATTERN.test(login)) return fail(400, { ok: false, code: 'missing' });
    return DEMO ? demoResolve(login) : twitchResolve(login);
  },
  testfetch: recipeAction(async ({ uid, form, locale }, { locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false, error: actionError(locale, 'Premium only while in beta.') });
    const reply = await rehearseFetchDef(uid, form);
    return reply.ok ? reply.data : fail(reply.status, actionErrorBody(locale, reply.body));
  })
};
