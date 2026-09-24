// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import type { PageServerLoad } from './$types';
import { resolveLogin } from '$lib/server/services';
import { readLoyalty, topStandings } from '$lib/server/loyalty-store';
import { listCommands, listModules } from '$lib/server/commands-store';
import { channelLabel, publicCommands, publicModules, type PublicCommand, type PublicModule } from '$lib/server/public-directory';

const LEADERBOARD_HOST = 'leaderboard.itsbagelbot.com';

const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

type Channel = { userId: string; login: string; channelName: string };

function requireLeaderboardHost(url: URL): void {
	if (dev || url.hostname === LEADERBOARD_HOST) return;
	throw error(404, 'Not found');
}

function requireLogin(segment: string): string {
	const login = (segment ?? '').replace(/^@+/, '').toLowerCase();
	if (!LOGIN_RE.test(login)) throw error(404, 'Channel not found');
	return login;
}

async function requireChannel(login: string): Promise<Channel> {
	const found = await resolveLogin(login).catch(() => null);
	if (!found?.userId) throw error(404, 'Channel not found');
	return { userId: found.userId, login, channelName: channelLabel(found, login) };
}

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

export const load: PageServerLoad = async ({ params, url, locals }) => {
	requireLeaderboardHost(url);

	const channel = await requireChannel(requireLogin(params.user ?? ''));
	let commands: PublicCommand[] = [];
	let modules: PublicModule[] = [];
	try {
		const [rows, mods] = await Promise.all([listCommands(channel.userId), listModules(channel.userId)]);
    commands = publicCommands(rows, locals.locale);
    modules = publicModules(mods, locals.locale);
	} catch {
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
