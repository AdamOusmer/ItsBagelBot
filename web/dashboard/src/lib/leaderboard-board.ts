// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LoyaltyStanding } from '@bagel/kit';

export const PAGE_SIZE = 15;
export const PODIUM_SIZE = 3;
export const MAX_QUERY_LENGTH = 50;
export const MAX_PAGES = 399;

export interface BoardParams {
  query: string;
  page: number;
}

export interface PageRequest {
  limit: number;
  offset: number;
  query: string;
}

export interface PageReply {
  rows: LoyaltyStanding[];
  total: number;
}

export interface BoardRange {
  from: number;
  to: number;
  total: number;
}

export interface BoardView {
  query: string;
  page: number;
  pages: number;
  rows: LoyaltyStanding[];
  range: BoardRange;
}

export type PageFetcher = (req: PageRequest) => Promise<PageReply>;

export function cleanQuery(raw: string | null): string {
  const squeezed = (raw ?? '').replace(/^[@\s]+/, '').replace(/\s+/g, ' ').trim();
  return Array.from(squeezed).slice(0, MAX_QUERY_LENGTH).join('').trim();
}

function cleanPage(raw: string | null): number {
  if (!raw || !/^\d{1,6}$/.test(raw)) return 1;
  return Math.min(Math.max(Number(raw), 1), MAX_PAGES);
}

export function boardParams(url: URL): BoardParams {
  return { query: cleanQuery(url.searchParams.get('q')), page: cleanPage(url.searchParams.get('page')) };
}

export function boardHref({ query, page }: BoardParams, pathname: string): string {
  const params = new URLSearchParams();
  if (query) params.set('q', query);
  if (page > 1) params.set('page', String(page));
  const qs = params.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function isCanonical(url: URL, { query, page }: BoardParams): boolean {
  const sameQuery = url.searchParams.get('q') === (query || null);
  const samePage = url.searchParams.get('page') === (page > 1 ? String(page) : null);
  return sameQuery && samePage;
}

export function canonicalLocation(url: URL): string | null {
  const params = boardParams(url);
  if (isCanonical(url, params)) return null;
  const next = new URLSearchParams(url.searchParams);
  next.delete('q');
  next.delete('page');
  if (params.query) next.set('q', params.query);
  if (params.page > 1) next.set('page', String(params.page));
  const qs = next.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

export interface EmptyCopy {
  title: string;
  body: string;
}

export function emptyCopyKeys(state: { query: string; degraded: boolean; podiumSize: number }): EmptyCopy | null {
  if (state.degraded) return null;
  if (state.query) return { title: 'leaderboard.noMatchesTitle', body: 'leaderboard.noMatchesBody' };
  if (state.podiumSize === 0) return { title: 'leaderboard.emptyTitle', body: 'leaderboard.emptyBody' };
  return { title: '', body: state.podiumSize === 1 ? 'leaderboard.soloNote' : 'leaderboard.podiumOnlyNote' };
}

function rankBase({ query }: BoardParams): number {
  return query ? 0 : PODIUM_SIZE;
}

function pageCount(matches: number): number {
  return Math.min(Math.max(1, Math.ceil(matches / PAGE_SIZE)), MAX_PAGES);
}

function pageRequest(params: BoardParams): PageRequest {
  return { limit: PAGE_SIZE, offset: rankBase(params) + (params.page - 1) * PAGE_SIZE, query: params.query };
}

function matchCount(params: BoardParams, reply: PageReply): number {
  return Math.max(0, reply.total - rankBase(params));
}

function rangeOf(params: BoardParams, reply: PageReply): BoardRange {
  const base = rankBase(params);
  const total = reply.total;
  if (reply.rows.length === 0) return { from: 0, to: 0, total };
  const from = base + (params.page - 1) * PAGE_SIZE + 1;
  return { from, to: from + reply.rows.length - 1, total };
}

function viewOf(params: BoardParams, reply: PageReply): BoardView {
  return {
    ...params,
    pages: pageCount(matchCount(params, reply)),
    rows: reply.rows,
    range: rangeOf(params, reply),
  };
}

export function emptyBoard(params: BoardParams): BoardView {
  return viewOf({ query: params.query, page: 1 }, { rows: [], total: 0 });
}

export async function loadBoard(params: BoardParams, fetchPage: PageFetcher): Promise<BoardView> {
  const first = await fetchPage(pageRequest(params));
  const pages = pageCount(matchCount(params, first));
  if (params.page <= pages) return viewOf(params, first);
  const last = { query: params.query, page: pages };
  return viewOf(last, await fetchPage(pageRequest(last)));
}
