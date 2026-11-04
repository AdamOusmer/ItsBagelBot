// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { Automation, RunRecord } from '@bagel/kit/automations';
import { getAutomation, listRuns } from '#lib/server/automations-store';
import {
  AUTOMATIONS_MODULE,
  DEMO,
  mutate,
  otherAutomations,
  readModuleEnabled,
  runAutomation,
  saveDraft,
  type OtherAutomation
} from '#lib/server/automations-actions';
import { moduleLoad } from '#lib/server/module-page';

const NEW_ID = 'new';

interface EditorView {
  enabled: boolean;
  automation: Automation | null;
  missing: boolean;
  runs: RunRecord[];
  others: OtherAutomation[];
}

const BLANK: EditorView = { enabled: false, automation: null, missing: false, runs: [], others: [] };

async function readEditor(uid: string, id: string): Promise<EditorView> {
  const creating = id === NEW_ID;
  const [enabled, automation, runs, others] = await Promise.all([
    readModuleEnabled(uid),
    creating ? null : getAutomation(uid, id),
    creating ? [] : listRuns(uid, id).catch(() => [] as RunRecord[]),
    otherAutomations(uid, id)
  ]);
  return { enabled, automation, missing: !creating && !automation, runs, others };
}

export const load: PageServerLoad = async ({ locals, params }) => ({
  id: params.id,
  ...(await moduleLoad(AUTOMATIONS_MODULE, locals.session, {
    read: (uid) => readEditor(uid, params.id),
    blank: () => BLANK
  }))
});

export const actions: Actions = {
  save: async (event) => {
    let savedId = '';
    const res = await mutate('save', 'Invalid automation.', (uid, f) => saveDraft(uid, f, (a) => (savedId = a.id)))(event);
    return 'ok' in res ? { ...res, id: savedId } : res;
  },
  run: mutate('run', 'Missing automation name.', runAutomation)
};
