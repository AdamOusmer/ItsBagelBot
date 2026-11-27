// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { MOD } from '@bagel/kit';
import { LOGIN_RE } from '@bagel/kit/twitch-names';
import { error, fail, redirect } from '@sveltejs/kit';
import { usesPremiumSteps } from '#lib/flow/model';
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
import { flowDenial, liveEditor, liveRewardSave, liveSave, lowerSaveRequest } from '#lib/server/flow-page';
import { goveeStore, type GoveeDevice } from '#lib/server/govee-store';
import { broadcasterPremium, gateModulePage } from '#lib/server/module-gate';

const recipeAction = signedInAction((session) => gateModulePage(session, MOD.automations));
const RUN_REFUSAL_STATUS = new Map([['invalid', 400], ['forbidden', 403], ['not_found', 404]]);

export type GoveeLights = { keyPresent: boolean; devices: GoveeDevice[]; code?: 'key_rejected' | 'unreachable' };

async function goveeLights(locals: App.Locals): Promise<GoveeLights> {
  const store = goveeStore(effectiveId(locals.session));
  const { keyPresent } = await store.read();
  if (!keyPresent) return { keyPresent, devices: [] };
  const { devices, code } = await store.listDevices();
  return { keyPresent, devices, code };
}

type Choices = {
  rewards: Promise<ChoiceList>;
  manageableRewards: Promise<ChoiceList>;
  counters: Promise<ChoiceList>;
  namedCooldowns: Promise<ChoiceList>;
  namedCountdowns: Promise<ChoiceList>;
  recipes: Promise<ChoiceList>;
};

function choicesFor(locals: App.Locals, ref: FlowRef | null): Choices {
  const current = ref?.layer === 'automation' ? ref.id : undefined;
  const uid = effectiveId(locals.session);
  return { rewards: rewardChoices(uid), manageableRewards: manageableRewardChoices(uid), counters: counterChoices(uid), namedCooldowns: namedCooldownChoices(uid), namedCountdowns: namedCountdownChoices(uid), recipes: recipeChoices(uid, current) };
}

export const load: PageServerLoad = async ({ params, locals, depends }) => {
  if (!(await broadcasterPremium(locals))) throw redirect(303, '/flows');
  depends('flows:govee', 'flows:choices');
  const editor = await liveEditor(locals, params.id);
  if (!editor) throw error(404);
  const govee = goveeLights(locals).catch((): GoveeLights => ({ keyPresent: true, devices: [], code: 'unreachable' }));
  return { ...editor, govee, ...choicesFor(locals, editor.ref) };
};

const DEVICE_PATTERN = /^[A-Za-z0-9:_-]{1,64}$/;

async function twitchResolve(login: string) {
  const found = await lookupTwitchUser(login);
  if (found.kind === 'missing') return fail(404, { ok: false, code: 'missing' });
  if (found.kind !== 'found') return fail(502, { ok: false, code: 'failed' });
  return { ok: true, userId: found.value.id, login: found.value.login };
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
      return { ok: true, runs: await listRuns(effectiveId(locals.session), ref.id) };
    } catch {
      return fail(502, { ok: false });
    }
  },
  save: async ({ request, locals }) => {
    const denied = await flowDenial(locals);
    if (denied) return denied;
    const lowered = lowerSaveRequest(String((await request.formData()).get('payload') ?? ''));
    if (!lowered.ok) return lowered.reply;
    if (usesPremiumSteps(lowered.lowered.flow) && !(await broadcasterPremium(locals))) {
      return { ok: false, reason: 'denied', error: actionError(locals.locale, 'Premium only.') };
    }
    return liveSave(locals, lowered.lowered);
  },
  run: async ({ request, locals }) => {
    const denied = await flowDenial(locals);
    if (denied) return fail(403, { ok: false, code: 'forbidden', error: denied.error });
    const form = await request.formData();
    const name = form.get('name');
    if (typeof name !== 'string' || !name.trim()) return fail(400, { ok: false, code: 'invalid' });
    return runSavedRecipe(locals, form);
  },
  reward: async ({ request, locals }) => {
    const denied = await flowDenial(locals);
    if (denied) return denied;
    const draft = parseRewardDraft(String((await request.formData()).get('payload') ?? ''));
    if (!draft) return fail(400, { ok: false, error: actionError(locals.locale, 'Invalid reward.') });
    const reply = await liveRewardSave(locals, draft);
    return reply.ok ? reply : fail(reply.status, reply);
  },
  scenes: async ({ request, locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false });
    const form = await request.formData();
    const device = String(form.get('device') ?? '');
    const sku = String(form.get('sku') ?? '');
    if (!DEVICE_PATTERN.test(device) || !DEVICE_PATTERN.test(sku)) return fail(400, { ok: false });
    return { ok: true, list: await sceneChoices(effectiveId(locals.session), { device, sku }) };
  },
  resolveUser: async ({ request, locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false, code: 'failed' });
    const login = String((await request.formData()).get('login') ?? '');
    if (!LOGIN_RE.test(login)) return fail(400, { ok: false, code: 'missing' });
    return twitchResolve(login);
  },
  testfetch: recipeAction(async ({ uid, form, locale }, { locals }) => {
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false, error: actionError(locale, 'Premium only while in beta.') });
    const reply = await rehearseFetchDef(uid, form);
    return reply.ok ? reply.data : fail(reply.status, actionErrorBody(locale, reply.body));
  })
};
