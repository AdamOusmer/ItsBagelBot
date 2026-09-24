// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LayoutServerLoad } from './$types';
import { loadGuildShell } from '$lib/server/discord-guild';

export const load: LayoutServerLoad = (event) => loadGuildShell(event);
