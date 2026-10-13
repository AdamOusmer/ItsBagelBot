// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import type { Locale } from '@bagel/kit/i18n';
import type { PageServerLoad } from './$types';
import { resolveLogin, userCommandsPage, userLeaderboardPage } from '$lib/server/services';
import { readLoyalty, topStandings } from '$lib/server/loyalty-store';
import { listCommands, listModules } from '$lib/server/commands-store';
import { channelLabel, publicCommands, publicModules, type PublicCommand, type PublicModule } from '$lib/server/public-directory';

const DEMO = dev && env.DEMO === '1';

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

type Standings = {
	currencyName: string;
	top: Awaited<ReturnType<typeof topStandings>>;
	degraded: boolean;
};

type Directory = { commands: PublicCommand[]; modules: PublicModule[] };

function hideChannel(locals: App.Locals): never {
	locals.edgeCache404 = true;
	throw error(404, 'Channel not found');
}

async function standings(userId: string, loyalty: ReturnType<typeof readLoyalty>): Promise<Standings> {
	try {
		const [view, top] = await Promise.all([loyalty, topStandings(userId, 50)]);
		return { currencyName: view.config.pointsName || 'points', top, degraded: false };
	} catch {
		return { currencyName: 'points', top: [], degraded: true };
	}
}

async function directory(userId: string, locale: Locale, visible: boolean): Promise<Directory> {
	if (!visible) return { commands: [], modules: [] };
	try {
		const [rows, mods] = await Promise.all([listCommands(userId), listModules(userId)]);
		return { commands: publicCommands(rows, locale), modules: publicModules(mods, locale) };
	} catch {
		return { commands: [], modules: [] };
	}
}

async function demoPage(login: string) {
	const d = await import('$lib/server/demo-data');
	return {
		login,
		channelName: login,
		currencyName: d.demoLeaderboard.currencyName,
		top: d.demoLeaderboard.top,
		degraded: false,
		commandsPage: true,
		commands: d.demoPublicCommands satisfies PublicCommand[],
		modules: d.demoPublicModules satisfies PublicModule[]
	};
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
	requireLeaderboardHost(url);

	const login = requireLogin(params.user ?? '');
	if (DEMO) return demoPage(login);

	const channel = await requireChannel(login);
	const loyalty = readLoyalty(channel.userId);
	const loyaltyState = loyalty.then((v) => v.enabled).catch(() => true);
	const [boardVisible, commandsPage, loyaltyEnabled] = await Promise.all([
		userLeaderboardPage(channel.userId).catch(() => true),
		userCommandsPage(channel.userId).catch(() => true),
		loyaltyState
	]);
	if (!boardVisible || !loyaltyEnabled) hideChannel(locals);

	const [{ commands, modules }, board] = await Promise.all([
		directory(channel.userId, locals.locale, commandsPage),
		standings(channel.userId, loyalty)
	]);

	return {
		login: channel.login,
		channelName: channel.channelName,
		currencyName: board.currencyName,
		top: board.top,
		degraded: board.degraded,
		commandsPage,
		commands,
		modules
	};
};
