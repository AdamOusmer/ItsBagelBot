// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The guild shell's data, loaded once for the whole section.
//
// It sits in the LAYOUT rather than in each page because the shell (header,
// sub-nav, banners) and every sub-page read the same three reads -- the config
// row, the guild layout and the bot status. As a +page.server.ts load it would
// re-run all three on every hop between Channels and Tickets, which is three
// RPCs per click for data that did not change; as a layout load SvelteKit
// reuses it across the sub-navigation and the pages just read `data`.
import type { LayoutServerLoad } from './$types';
import { loadGuildShell } from '$lib/server/discord-guild';

export const load: LayoutServerLoad = (event) => loadGuildShell(event);
