// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import type { PageServerLoad } from './$types';
import { userCommandsPage, userLeaderboardPage } from '$lib/server/services';
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
import { channelLabel } from '$lib/server/public-directory';
import { markEdge, pageOutcome, readVisibility } from '$lib/server/edge-cache';
import { lookupLogin } from '$lib/server/channel-lookup';
import { leaderboardCacheTag } from '$lib/edge-tags';
import { LOGIN_RE, normalizeLogin } from '$lib/public-login';

const DEMO = dev && env.DEMO === '1';

const LEADERBOARD_HOST = 'leaderboard.itsbagelbot.com';


type Channel = { userId: string; login: string; channelName: string };

function requireLeaderboardHost(url: URL, locals: App.Locals): void {
	if (dev || url.hostname === LEADERBOARD_HOST) return;
	markEdge(locals, 'missing');
	throw error(404, 'Not found');
}

function channelNotFound(): never {
	throw error(404, 'Channel not found');
}

function requireLogin(segment: string, locals: App.Locals): string {
	const login = normalizeLogin(segment);
	if (LOGIN_RE.test(login)) return login;
	markEdge(locals, 'missing');
	return channelNotFound();
}

async function requireChannel(login: string, locals: App.Locals): Promise<Channel> {
	const looked = await lookupLogin(login);
	if (typeof looked === 'object') return { userId: looked.userId, login, channelName: channelLabel(looked, login) };
	if (looked === 'missing') markEdge(locals, 'missing');
	return channelNotFound();
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
	markEdge(locals, 'hidden');
	return channelNotFound();
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
	requireLeaderboardHost(url, locals);
	const login = requireLogin(segment, locals);
	locals.edgeCacheTag = leaderboardCacheTag(login);
	const spelled = segment === login || segment === `@${login}`;
	const canonical = canonicalLocation(url);
	if (spelled && !canonical) return login;
	const search = new URL(canonical ?? `${url.pathname}${url.search}`, url).search;
	throw redirect(308, `${spelled ? url.pathname : `/${login}`}${search}`);
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
	const login = enter(url, params.user ?? '', locals);
	const view = boardParams(url);
	if (DEMO) return demoPage(login, view);

	const channel = await requireChannel(login, locals);
	const loyalty = readLoyalty(channel.userId);
	const board = standings(channel.userId, loyalty, view);
	const [boardVisible, commandsPage, loyaltyEnabled] = await Promise.all([
		readVisibility(userLeaderboardPage(channel.userId)),
		readVisibility(userCommandsPage(channel.userId)),
		readVisibility(loyalty.then((v) => v.enabled))
	]);
	if (boardVisible === false || loyaltyEnabled === false) hideChannel(locals);

	const standing = await board;
	markEdge(locals, pageOutcome([boardVisible, commandsPage], standing.degraded || standing.busy));

	return {
		login: channel.login,
		channelName: channel.channelName,
		currencyName: standing.currencyName,
		podium: standing.podium,
		...standing.board,
		degraded: standing.degraded,
		busy: standing.busy,
		commandsPage: commandsPage !== false
	};
};
