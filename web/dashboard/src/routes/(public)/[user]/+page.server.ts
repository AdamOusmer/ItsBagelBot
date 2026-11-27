// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { RpcError } from '@bagel/kit/server/nats';
import type { PageServerLoad } from './$types';
import { userCommandsPage, userLeaderboardPage } from '#lib/server/services';
import {
	readLoyalty,
	standingsPage,
	topStandings,
	type FreezeStamp,
	type StandingsPage,
	type StandingsPageRequest,
	type StandingsPin
} from '#lib/server/loyalty-store';
import { publicGiveawayWinners } from '#lib/server/channel-giveaways';
import { resolveCurrentViewer } from '#lib/server/viewer-lookup';
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
} from '#lib/leaderboard-board';
import { channelLabel } from '#lib/server/public-directory';
import { markEdge, pageOutcome, readVisibility } from '#lib/server/edge-cache';
import { lookupLogin } from '#lib/server/channel-lookup';
import { leaderboardCacheTag } from '#lib/edge-tags';
import { LOGIN_RE, normalizeLogin } from '#lib/public-login';

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
	podium: StandingsPage['rows'];
	board: BoardView;
	freeze: FreezeStamp | null;
	degraded: boolean;
	busy: boolean;
};

type Search = { busy: boolean };
type ReadBoard = (req: StandingsPageRequest) => Promise<StandingsPage>;

function emptyStandings(params: BoardParams, degraded: boolean): Standings {
	return { currencyName: 'points', podium: [], freeze: null, board: emptyBoard(params), degraded, busy: false };
}

function hideChannel(locals: App.Locals): never {
	markEdge(locals, 'hidden');
	return channelNotFound();
}

async function loginPage(req: PageRequest, search: Search, read: ReadBoard): Promise<PageReply> {
	const { limit, offset, query } = req;
	const stored = await read({ limit, offset, viewerLogin: query });
	if (stored.freeze || stored.rows.length > 0) return stored;
	const resolved = await resolveCurrentViewer(query);
	search.busy = resolved.kind === 'busy';
	if (resolved.kind !== 'found') return stored;
	return read({ limit, offset, viewerId: resolved.viewerId, viewerLogin: query });
}

function searchPage(req: PageRequest, search: Search, read: ReadBoard): Promise<PageReply> {
	const { limit, offset, query } = req;
	if (!query) return read({ limit, offset });
	if (!isSearchable(query)) return Promise.resolve({ rows: [], total: 0 });
	if (isViewerId(query)) return read({ limit, offset, viewerId: query });
	return loginPage(req, search, read);
}

function pinTo(freeze: FreezeStamp | null): StandingsPin {
	return freeze ? { freezeId: freeze.id } : { live: true };
}

async function loadStandings(userId: string, params: BoardParams, podium?: StandingsPage) {
	const search: Search = { busy: false };
	let pin = podium ? pinTo(podium.freeze) : undefined;
	const read: ReadBoard = async (req) => {
		const page = await standingsPage(userId, { ...req, ...pin });
		pin ??= pinTo(page.freeze);
		return page;
	};
	const board = await loadBoard(params, (req) => searchPage(req, search, read));
	const top = podium ?? await topStandings(userId, PODIUM_SIZE, pin);
	return { podium: top.rows, freeze: top.freeze, board, busy: search.busy };
}

async function pinnedStandings(userId: string, params: BoardParams) {
	const podium = await topStandings(userId, PODIUM_SIZE);
	try {
		return await loadStandings(userId, params, podium);
	} catch (err) {
		if (!(err instanceof RpcError) || err.code !== 'conflict') throw err;
		return loadStandings(userId, params);
	}
}

async function standings(userId: string, loyalty: ReturnType<typeof readLoyalty>, params: BoardParams): Promise<Standings> {
	try {
		const [view, standing] = await Promise.all([loyalty, pinnedStandings(userId, params)]);
		return { currencyName: view.config.pointsName || 'points', ...standing, degraded: false };
	} catch {
		return emptyStandings(params, true);
	}
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

async function publicFeatures(channel: Channel, view: BoardParams, locals: App.Locals) {
	const loyalty = readLoyalty(channel.userId);
	const [boardVisible, commandsPage, loyaltyEnabled] = await Promise.all([
		readVisibility(userLeaderboardPage(channel.userId)),
		readVisibility(userCommandsPage(channel.userId)),
		readVisibility(loyalty.then((v) => v.enabled))
	]);
	if (boardVisible === false) hideChannel(locals);

	const showStandings = loyaltyEnabled !== false;
	const [standing, giveaways] = await Promise.all([
		showStandings ? standings(channel.userId, loyalty, view) : emptyStandings(view, false),
		boardVisible ? publicGiveawayWinners(channel.userId) : { winners: [], degraded: true }
	]);
	if (!showStandings && !hasWinnerSection(giveaways)) hideChannel(locals);
	return { boardVisible, commandsPage, showStandings, standing, giveaways };
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
	const login = enter(url, params.user ?? '', locals);
	const view = boardParams(url);

	const channel = await requireChannel(login, locals);
	const { boardVisible, commandsPage, showStandings, standing, giveaways } = await publicFeatures(channel, view, locals);
	const degraded = standing.degraded || standing.busy || giveaways.degraded;
	markEdge(locals, pageOutcome([boardVisible, commandsPage], degraded));
	const freeze = standing.freeze;
	if (freeze) locals.edgeTtlCapSec = Math.max(1, Math.floor((freeze.expiresAt - Date.now()) / 1000));

	return {
		login: channel.login,
		channelName: channel.channelName,
		currencyName: standing.currencyName,
		podium: standing.podium,
		frozen: freeze ? { asOf: freeze.asOf } : null,
		...standing.board,
		degraded: standing.degraded,
		busy: standing.busy,
		commandsPage: commandsPage !== false,
		showStandings,
		giveawayWinners: giveaways.winners,
		giveawayHistoryUnavailable: giveaways.degraded
	};
};

function hasWinnerSection(giveaways: { winners: unknown[]; degraded: boolean }): boolean {
	return giveaways.winners.length > 0 || giveaways.degraded;
}
