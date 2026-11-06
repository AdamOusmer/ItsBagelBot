// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { Automation } from '@bagel/kit/automations';
import { listAutomations } from '#lib/server/automations-store';
import {
  AUTOMATIONS_MODULE,
  flipEnabled,
  mutate,
  readModuleEnabled,
  removeAutomation,
  runAutomation,
  toggleModule
} from '#lib/server/automations-actions';
import { moduleLoad } from '#lib/server/module-page';

export const load: PageServerLoad = ({ locals }) =>
  moduleLoad(AUTOMATIONS_MODULE, locals.session, {
    read: async (uid) => {
      const [enabled, automations] = await Promise.all([readModuleEnabled(uid), listAutomations(uid)]);
      return { enabled, automations };
    },
    blank: () => ({ enabled: false, automations: [] as Automation[] })
  });

export const actions: Actions = {
  toggle: toggleModule,
  setEnabled: mutate('set_enabled', 'Invalid input.', flipEnabled),
  remove: mutate('remove', 'Missing automation id.', removeAutomation),
  run: mutate('run', 'Missing automation name.', runAutomation)
};
