// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import type { PageServerLoad } from './$types';
import { resolveLogin, userCommandsPage, userLeaderboardPage } from '$lib/server/services';
import { readLoyalty, standingsPage, topStandings } from '$lib/server/loyalty-store';
import { resolveCurrentViewer } from '$lib/server/viewer-lookup';
import {
	boardParams,
	canonicalLocation,
	emptyBoard,
	isSearchable,
	isViewerId,
	loadBoard,
	PODIUM_SIZE,
	type BoardParams,
	type BoardView,
	type PageReply,
	type PageRequest
} from '$lib/leaderboard-board';
import { leaderboardCacheTag } from '$lib/edge-tags';
import { channelLabel } from '$lib/server/public-directory';

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
	podium: Awaited<ReturnType<typeof topStandings>>;
	board: BoardView;
	degraded: boolean;
	busy: boolean;
};

type Search = { busy: boolean };

function hideChannel(locals: App.Locals): never {
	locals.edgeCache404 = true;
	throw error(404, 'Channel not found');
}

function skipEdgeCache(locals: App.Locals, board: Standings): void {
	if (board.degraded || board.busy) locals.edgeCacheSkip = true;
}

async function loginPage(userId: string, req: PageRequest, search: Search): Promise<PageReply> {
	const { limit, offset, query } = req;
	const stored = await standingsPage(userId, { limit, offset, viewerLogin: query });
	if (stored.rows.length > 0) return stored;
	const resolved = await resolveCurrentViewer(query);
	search.busy = resolved.kind === 'busy';
	if (resolved.kind !== 'found') return stored;
	return standingsPage(userId, { limit, offset, viewerId: resolved.viewerId, viewerLogin: query });
}

function searchPage(userId: string, req: PageRequest, search: Search): Promise<PageReply> {
	const { limit, offset, query } = req;
	if (!query) return standingsPage(userId, { limit, offset });
	if (!isSearchable(query)) return Promise.resolve({ rows: [], total: 0 });
	if (isViewerId(query)) return standingsPage(userId, { limit, offset, viewerId: query });
	return loginPage(userId, req, search);
}

async function standings(userId: string, loyalty: ReturnType<typeof readLoyalty>, params: BoardParams): Promise<Standings> {
	const search: Search = { busy: false };
	try {
		const [view, podium] = await Promise.all([loyalty, topStandings(userId, PODIUM_SIZE)]);
		const board = await loadBoard(params, (req) => searchPage(userId, req, search));
		return { currencyName: view.config.pointsName || 'points', podium, board, degraded: false, busy: search.busy };
	} catch {
		return { currencyName: 'points', podium: [], board: emptyBoard(params), degraded: true, busy: false };
	}
}

async function demoPage(login: string, params: BoardParams) {
	const d = await import('$lib/server/demo-data');
	return {
		login,
		channelName: login,
		currencyName: d.demoLeaderboard.currencyName,
		podium: d.demoPodium(),
		...(await loadBoard(params, async (req) => d.demoStandingsPage(req))),
		degraded: false,
		busy: false,
		commandsPage: true
	};
}

function enter(url: URL, segment: string, locals: App.Locals): string {
	requireLeaderboardHost(url);
	const login = requireLogin(segment);
	locals.edgeCacheTag = leaderboardCacheTag(login);
	const canonical = canonicalLocation(url);
	if (canonical) throw redirect(308, canonical);
	return login;
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
	const login = enter(url, params.user ?? '', locals);
	const view = boardParams(url);
	if (DEMO) return demoPage(login, view);

	const channel = await requireChannel(login);
	const loyalty = readLoyalty(channel.userId);
	const loyaltyState = loyalty.then((v) => v.enabled).catch(() => true);
	const [boardVisible, commandsPage, loyaltyEnabled] = await Promise.all([
		userLeaderboardPage(channel.userId).catch(() => true),
		userCommandsPage(channel.userId).catch(() => true),
		loyaltyState
	]);
	if (!boardVisible || !loyaltyEnabled) hideChannel(locals);

	const board = await standings(channel.userId, loyalty, view);
	skipEdgeCache(locals, board);

	return {
		login: channel.login,
		channelName: channel.channelName,
		currencyName: board.currencyName,
		podium: board.podium,
		...board.board,
		degraded: board.degraded,
		busy: board.busy,
		commandsPage
	};
};
