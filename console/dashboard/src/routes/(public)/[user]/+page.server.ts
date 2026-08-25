// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import type { PageServerLoad } from './$types';
import { resolveLogin } from '$lib/server/services';
import { readLoyalty, topStandings } from '$lib/server/loyalty-store';
import { listCommands, listModules } from '$lib/server/commands-store';
import { publicCommands, publicModules, type PublicCommand, type PublicModule } from '$lib/server/public-directory';

// The root-level [user] segment exists for exactly one surface: the
// leaderboard.itsbagelbot.com/<user> host. Every other hostname keeps its old
// routing semantics — an unmatched single-segment path 404s as before —
// because this route answers only on the leaderboard host (plus dev, so the
// page can be built against localhost). Without this guard the dashboard host
// would start resolving arbitrary paths as Twitch logins.
const LEADERBOARD_HOST = 'leaderboard.itsbagelbot.com';

// Twitch login shape: letters, digits and underscore, 25 max — same gate the
// public channel page applies. Anything else cannot name a channel, so it is
// a 404 rather than a lookup.
const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

type Channel = { userId: string; login: string; channelName: string };

/** Serves the page on the leaderboard host (and dev) only. */
function requireLeaderboardHost(url: URL): void {
	if (dev || url.hostname === LEADERBOARD_HOST) return;
	throw error(404, 'Not found');
}

/** The segment's login reading, or a 404 when it cannot name a channel. */
function requireLogin(segment: string): string {
	const login = (segment ?? '').replace(/^@+/, '').toLowerCase();
	if (!LOGIN_RE.test(login)) throw error(404, 'Channel not found');
	return login;
}

/** The channel behind a login, or a 404 when the users service has none. */
async function requireChannel(login: string): Promise<Channel> {
	const found = await resolveLogin(login).catch(() => null);
	if (!found?.userId) throw error(404, 'Channel not found');
	return { userId: found.userId, login, channelName: found.username || login };
}

/** The channel's top standings, degrading to an empty board over an outage. */
async function standings(userId: string): Promise<{
	currencyName: string;
	top: Awaited<ReturnType<typeof topStandings>>;
	degraded: boolean;
}> {
	try {
		const [view, top] = await Promise.all([readLoyalty(userId), topStandings(userId, 50)]);
		return { currencyName: view.config.pointsName || 'points', top, degraded: false };
	} catch {
		return { currencyName: 'points', top: [], degraded: true };
	}
}

export const load: PageServerLoad = async ({ params, url }) => {
	requireLeaderboardHost(url);

	const channel = await requireChannel(requireLogin(params.user ?? ''));
	// Commands degrade to empty alongside the board: an outage in one store
	// must not blank the whole page.
	let commands: PublicCommand[] = [];
	let modules: PublicModule[] = [];
	try {
		const [rows, mods] = await Promise.all([listCommands(channel.userId), listModules(channel.userId)]);
		commands = publicCommands(rows);
		modules = publicModules(mods);
	} catch {
		/* the leaderboard itself still renders */
	}
	const board = await standings(channel.userId);

	return {
		login: channel.login,
		channelName: channel.channelName,
		currencyName: board.currencyName,
		top: board.top,
		degraded: board.degraded,
		commands,
		modules
	};
};
