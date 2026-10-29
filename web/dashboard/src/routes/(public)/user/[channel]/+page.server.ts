// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import type { Locale } from '@bagel/kit/i18n';
import type { PageServerLoad } from './$types';
import { listCommands, listModules } from '#lib/server/commands-store';
import { channelLabel, publicCommands, publicModules, type PublicCommand, type PublicModule } from '#lib/server/public-directory';
import { accountState, userCommandsPage, type ResolvedChannel } from '#lib/server/services';
import { markEdge, pageOutcome, readVisibility } from '#lib/server/edge-cache';
import { lookupLogin } from '#lib/server/channel-lookup';
import { requireHost } from '#lib/server/seo-hosts';
import { LOGIN_RE, normalizeLogin } from '#lib/public-login';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';

const DEMO = dev && env.DEMO === '1';

const ID_RE = /^[1-9]\d{0,19}$/;

type Segment = { login: string | null; id: string | null };

function parseSegment(raw: string): Segment {
  const cleaned = normalizeLogin(raw ?? '');
  return {
    login: LOGIN_RE.test(cleaned) ? cleaned : null,
    id: ID_RE.test(raw) ? raw : null
  };
}

// Edge purges list exact URLs, so every other spelling of a channel redirects to one channelPageUrls covers.
function requireCanonicalSegment(raw: string, segment: Segment, url: URL): void {
  const login = segment.login;
  if (!login || [login, `@${login}`, segment.id].includes(raw)) return;
  throw redirect(308, `/user/${login}${url.search}`);
}

type Channel = { userId: string; channelName: string };

function canonicalLogin(record: { username?: string | null } | null): string | null {
  const login = (record?.username ?? '').toLowerCase();
  return LOGIN_RE.test(login) ? login : null;
}

function channelFromRecord(found: ResolvedChannel, login: string): Channel {
  const canonical = canonicalLogin(found) ?? login;
  if (canonical !== login) throw redirect(308, `/user/${canonical}`);
  return { userId: found.userId, channelName: channelLabel(found, login) };
}

async function channelFromID(userId: string): Promise<Channel> {
  const account = await accountState(userId).catch(() => null);
  const canonical = canonicalLogin(account);
  if (canonical) throw redirect(308, `/user/${canonical}`);
  return { userId, channelName: `channel ${userId}` };
}

// Same message for hidden, absent and unreadable, so a probe cannot tell them apart.
function channelNotFound(): never {
  throw error(404, 'Channel not found');
}

async function resolveChannel(segment: Segment, locals: App.Locals): Promise<Channel> {
  const looked = segment.login ? await lookupLogin(segment.login) : 'missing';
  if (typeof looked === 'object') return channelFromRecord(looked, segment.login ?? '');
  if (segment.id) return channelFromID(segment.id);
  if (looked === 'missing') markEdge(locals, 'missing');
  return channelNotFound();
}

type Directory = {
  channelName: string;
  creatorCode: string | null;
  commands: PublicCommand[];
  modules: PublicModule[];
  degraded: boolean;
};

async function readDirectory(channel: Channel, locale: Locale): Promise<Directory> {
  try {
    const [commands, modules, account] = await Promise.all([
      listCommands(channel.userId),
      listModules(channel.userId),
      accountState(channel.userId).catch(() => null)
    ]);
    return {
      channelName: channelLabel(account, channel.channelName),
      creatorCode: account?.creatorCode ?? null,
      commands: publicCommands(commands, locale),
      modules: publicModules(modules, locale),
      degraded: false
    };
  } catch {
    return { channelName: channel.channelName, creatorCode: null, commands: [], modules: [], degraded: true };
  }
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
  requireHost(url, 'commands');

  if (DEMO) {
    const d = await import('#lib/server/demo-data');
    return {
      userId: '1',
      channelName: parseSegment(params.channel).login ?? 'demo',
      creatorCode: d.demoCreatorCode,
      commands: d.demoPublicCommands satisfies PublicCommand[],
      modules: d.demoPublicModules satisfies PublicModule[],
      degraded: false
    };
  }

  const segment = parseSegment(params.channel);
  requireCanonicalSegment(params.channel, segment, url);
  const channel = await resolveChannel(segment, locals);
  const directory = readDirectory(channel, locals.locale);
  const visible = await readVisibility(userCommandsPage(channel.userId));
  if (visible === false) {
    markEdge(locals, 'hidden');
    return channelNotFound();
  }

  const page = await directory;
  markEdge(locals, pageOutcome([visible], page.degraded));
  return { userId: channel.userId, ...page };
};
