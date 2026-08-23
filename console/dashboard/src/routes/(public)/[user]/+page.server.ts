// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import type { PageServerLoad } from './$types';
import { resolveLogin } from '$lib/server/services';
import { readLoyalty, topStandings } from '$lib/server/loyalty-store';

// The root-level [user] segment exists for exactly one surface: the
// leaderboard.itsbagelbot.com/<user> host. Every other hostname keeps its old
// routing semantics — an unmatched single-segment path 404s as before — because
// this route answers only on the leaderboard host (plus dev, so the page can be
// built against localhost). Without this guard the dashboard host would start
// resolving arbitrary paths as Twitch logins.
const LEADERBOARD_HOST = 'leaderboard.itsbagelbot.com';

// Twitch login shape: letters, digits and underscore, 25 max — same gate the
// public channel page applies. Anything else cannot name a channel, so it is a
// 404 rather than a lookup.
const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

export const load: PageServerLoad = async ({ params, url }) => {
	if (!dev && url.hostname !== LEADERBOARD_HOST) {
		throw error(404, 'Not found');
	}

	const login = (params.user ?? '').replace(/^@+/, '').toLowerCase();
	if (!LOGIN_RE.test(login)) {
		throw error(404, 'Channel not found');
	}

	const found = await resolveLogin(login).catch(() => null);
	if (!found?.userId) {
		throw error(404, 'Channel not found');
	}
	const userId = found.userId;
	const channelName = found.username || login;

	try {
		const [view, top] = await Promise.all([readLoyalty(userId), topStandings(userId, 50)]);
		return {
			login,
			channelName,
			currencyName: view.config.pointsName || 'points',
			top,
			degraded: false
		};
	} catch {
		return {
			login,
			channelName,
			currencyName: 'points',
			top: [],
			degraded: true
		};
	}
};
