// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { fail } from '@sveltejs/kit';
import { classifyFlow, parseDefinition, type Automation, type Issue } from '@bagel/kit/automations';
import {
  deleteAutomation,
  listAutomations,
  runNow,
  setAutomationEnabled,
  upsertAutomation,
  type AutomationInput,
  type UpsertResult
} from './automations-store';
import { moduleDef } from '@bagel/kit/catalog';
import { moduleAction, type ModuleMutation } from './module-action';
import { readModuleBlob, setModuleEnabled } from './module-blob';
import { moduleWriteDenial } from './module-gate';

export const AUTOMATIONS_MODULE = 'automations';

export const DEMO = dev && process.env.DEMO === '1';

export function mutate(op: string, invalid: string, run: ModuleMutation) {
  const action = moduleAction(AUTOMATIONS_MODULE, op, run, { demo: DEMO, invalid });
  return async (event: Parameters<typeof action>[0]) => {
    const denied = event.locals.session ? await moduleWriteDenial(event.locals, moduleDef(AUTOMATIONS_MODULE)!) : null;
    return denied ?? action(event);
  };
}

export function refuse(code: string, issues: Issue[] = [], conflict = false) {
  return fail(conflict ? 409 : 400, { ok: false, code, issues, conflict });
}

function upsertRefusal(res: Extract<UpsertResult, { ok: false }>) {
  return refuse(res.conflict ? 'conflict' : (res.code ?? 'failed'), res.issues, res.conflict);
}

export async function readModuleEnabled(userId: string): Promise<boolean> {
  return (await readModuleBlob(userId, AUTOMATIONS_MODULE)).enabled;
}

export const toggleModule = mutate('toggle', 'Invalid input.', async (uid, f) => {
  const enabled = f.get('is_enabled') === 'on';
  await setModuleEnabled(uid, AUTOMATIONS_MODULE, enabled);
  return String(enabled);
});

function parseRevision(raw: FormDataEntryValue | null): number | undefined {
  const n = Number(raw);
  return raw !== null && raw !== '' && Number.isSafeInteger(n) ? n : undefined;
}

export function parseDraft(f: FormData): AutomationInput | null {
  const name = String(f.get('name') ?? '');
  const { definition } = parseDefinition(String(f.get('definition') ?? ''));
  if (!definition) return null;
  return {
    id: String(f.get('id') ?? '') || undefined,
    name,
    enabled: f.get('enabled') === 'on',
    definition,
    expectedRevision: parseRevision(f.get('revision'))
  };
}

export async function saveDraft(uid: string, f: FormData, saved: (a: Automation) => void) {
  const draft = parseDraft(f);
  if (!draft) return null;
  if (classifyFlow(draft.definition).layer !== 'automation') {
    return refuse('belongs_elsewhere', [{ path: 'definition', code: 'belongs_elsewhere' }]);
  }
  const res = await upsertAutomation(uid, draft);
  if (!res.ok) return upsertRefusal(res);
  saved(res.automation);
  return res.automation.name;
}

export async function flipEnabled(uid: string, f: FormData) {
  const id = String(f.get('id') ?? '');
  if (!id) return null;
  const res = await setAutomationEnabled(uid, id, f.get('enabled') === 'on', parseRevision(f.get('revision')));
  if (res.ok) return res.automation.name;
  return res.code === 'not_found' ? refuse('not_found') : upsertRefusal(res);
}

export const removeAutomation: ModuleMutation = async (uid, f) => {
  const id = String(f.get('id') ?? '');
  if (!id) return null;
  return (await deleteAutomation(uid, id)) ? id : refuse('not_found');
};

export const runAutomation: ModuleMutation = async (uid, f) => {
  const name = String(f.get('name') ?? '');
  if (!name) return null;
  const res = await runNow(uid, name);
  return res.ok ? name : refuse(res.code ?? 'failed');
};

export interface OtherAutomation {
  id: string;
  name: string;
}

export async function otherAutomations(uid: string, exceptId: string): Promise<OtherAutomation[]> {
  const all = await listAutomations(uid).catch(() => [] as Automation[]);
  return all.filter((a) => a.id !== exceptId).map((a) => ({ id: a.id, name: a.name }));
}
