// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MOD, type ModuleDef } from '@bagel/kit';
import { alertVoiceKeys } from '@bagel/kit/voice-plan';
import { fail } from '@sveltejs/kit';
import { logger } from '@bagel/kit/server/logger';
import { thrownStatus } from '@bagel/kit/server/form-action';
import { patchModule } from './commands-store';
import { auditDashboardImpersonation } from './services';
import { parentIsEnabled } from './module-parent';
import { attachLinkedUUID } from './minecraft-uuid';
import { effectiveId } from './board';
import { actionError } from './action-errors';
import { broadcasterPremium } from './module-gate';
import type { Session } from './session';

export function valueKeys(def: ModuleDef): string[] {
  const rules = def.id === MOD.triggers ? ['rules'] : [];
  return [...def.replies.map((r) => r.messageKey), ...(def.settings ?? []).map((s) => s.key), ...rules];
}

export function enableKeys(def: ModuleDef): string[] {
  return def.replies.flatMap((r) => (r.enableKey ? [r.enableKey] : []));
}

export async function gatedEnabled(uid: string, def: ModuleDef, requested: boolean): Promise<boolean> {
  if (!requested || !def.parent) return requested;
  return parentIsEnabled(uid, def.parent);
}

export async function patchModuleSettings(def: ModuleDef, form: FormData, locals: App.Locals, demo: boolean) {
  const voiceKeys = def.replies.flatMap((r) => r.voiceAlert ? [alertVoiceKeys(r.voiceAlert).enabled] : []);
  const allowed = new Set([...valueKeys(def), ...enableKeys(def), ...voiceKeys]);
  const partial = parsePartial(form.get('partial'), allowed);
  if (!partial) return { result: fail(400, { ok: false, error: actionError(locals.locale, 'Invalid patch.') }), freshRead: false };
  const denial = await voicePatchDenial(voiceKeys, partial, locals);
  if (denial) return { result: denial, freshRead: false };

  const requested = form.get('is_enabled') === 'on';
  const expectedRev = Number(form.get('expected_rev') ?? '0') || 0;
  if (demo) return { result: { ok: true, rev: expectedRev + 1, conflict: false }, freshRead: false };

  const uid = effectiveId(locals.session);
  const enabled = await gatedEnabled(uid, def, requested);
  const config = await attachLinkedUUID(def, partial, locals);
  return applyPatch(def, uid, { enabled, expectedRev, partial: config }, locals.session);
}

async function applyPatch(
  def: ModuleDef,
  uid: string,
  draft: { enabled: boolean; expectedRev: number; partial: Record<string, string> },
  session: Session | null | undefined
) {
  try {
    const res = await patchModule({
      userId: uid,
      name: def.id,
      isEnabled: draft.enabled,
      partial: draft.partial,
      expectedRev: draft.expectedRev
    });
    if (res.conflict) return { result: { ok: false, conflict: true, rev: res.rev }, freshRead: true };
    auditDashboardImpersonation(session, 'module:patch', `${def.id}=${draft.enabled}`);
    return { result: { ok: true, rev: res.rev, conflict: false }, freshRead: !res.synced };
  } catch (e) {
    logger.error({ err: e }, `[modules] patch ${def.id} failed`);
    return { result: fail(thrownStatus(e), { ok: false }), freshRead: false };
  }
}

async function voicePatchDenial(keys: readonly string[], partial: Record<string, string>, locals: App.Locals) {
  const values = keys.filter((key) => Object.hasOwn(partial, key)).map((key) => partial[key]);
  if (values.length === 0) return null;
  if (!(await broadcasterPremium(locals))) {
    return fail(403, { ok: false, error: actionError(locals.locale, 'Premium only while in beta.') });
  }
  if (values.some((value) => value !== 'true' && value !== 'false')) {
    return fail(400, { ok: false, error: actionError(locals.locale, 'Invalid patch.') });
  }
  return null;
}

function parsePartial(raw: FormDataEntryValue | null, allowed: ReadonlySet<string>): Record<string, string> | null {
  try {
    const obj = JSON.parse(String(raw ?? '{}'));
    if (!obj || typeof obj !== 'object') return {};
    const entries = Object.entries(obj as Record<string, unknown>).filter(([k]) => allowed.has(k));
    return Object.fromEntries(entries.map(([k, v]) => [k, v == null ? '' : String(v)]));
  } catch {
    return null;
  }
}
