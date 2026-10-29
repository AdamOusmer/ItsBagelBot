// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { guildActions, loadGuildModule, moduleActions } from '#lib/server/discord-guild';

const MODULE_ID = 'tickets';

// moduleActions read the module from params.id, which this fixed route does not have.
function forTickets(action: NonNullable<Actions[string]>) {
  return (event: Parameters<typeof action>[0]) =>
    action(Object.create(event, { params: { value: { ...event.params, id: MODULE_ID } } }) as typeof event);
}

export const load: PageServerLoad = (event) => loadGuildModule(event, MODULE_ID);

export const actions: Actions = {
  save: forTickets(moduleActions.save),
  toggle: forTickets(moduleActions.toggle),
  repost: guildActions.repost
};
