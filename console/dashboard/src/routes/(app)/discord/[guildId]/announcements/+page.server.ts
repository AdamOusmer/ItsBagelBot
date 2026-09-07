// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// No load: +layout.server.ts loads the guild shell once for every page here.
//
// The whole action table is re-exported rather than a subset, because a form
// posted from one sub-page can legitimately reach another's action (the dirty
// guard can resubmit, and `save` is shared by all of them). `save` merges a
// PARTIAL draft through mergeDiscordConfig, so a page posting only its own
// slice of DiscordConfig keys leaves every other field at its stored value.
import type { Actions } from './$types';
import { guildActions } from '$lib/server/discord-guild';

export const actions: Actions = guildActions;
