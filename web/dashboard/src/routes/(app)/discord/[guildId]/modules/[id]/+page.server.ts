// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { discordModuleById } from '@bagel/kit';
import type { Actions, PageServerLoad } from './$types';
import { loadGuildModule, moduleActions } from '#lib/server/discord-guild';

export const load: PageServerLoad = (event) => {
  const dedicated = discordModuleById(event.params.id)?.href;
  if (dedicated) throw redirect(307, `/discord/${event.params.guildId}/${dedicated}`);
  return loadGuildModule(event, event.params.id);
};

export const actions: Actions = moduleActions;
