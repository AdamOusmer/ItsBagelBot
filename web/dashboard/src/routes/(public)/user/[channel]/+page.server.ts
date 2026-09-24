// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import { listCommands, listModules } from '$lib/server/commands-store';
import { channelLabel, publicCommands, publicModules, type PublicCommand, type PublicModule } from '$lib/server/public-directory';
import { accountState, resolveLogin, userCommandsPage } from '$lib/server/services';
import { requireHost } from '$lib/server/seo-hosts';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

const LOGIN_RE = /^[a-z0-9_]{1,25}$/;
const ID_RE = /^[1-9]\d{0,19}$/;

type Segment = { login: string | null; id: string | null };

function parseSegment(raw: string): Segment {
  const cleaned = (raw ?? '').replace(/^@+/, '').toLowerCase();
  return {
    login: LOGIN_RE.test(cleaned) ? cleaned : null,
    id: ID_RE.test(raw) ? raw : null
  };
}

type Channel = { userId: string; channelName: string };

function canonicalLogin(record: { username?: string | null } | null): string | null {
  const login = (record?.username ?? '').toLowerCase();
  return LOGIN_RE.test(login) ? login : null;
}

async function channelFromLogin(segment: Segment): Promise<Channel | null> {
  const login = segment.login;
  if (!login) return null;

  const found = await resolveLogin(login).catch(() => null);
  if (!found?.userId) return null;

  const canonical = canonicalLogin(found) ?? login;
  if (canonical !== login) throw redirect(308, `/user/${canonical}`);
  return { userId: found.userId, channelName: channelLabel(found, login) };
}

async function channelFromID(segment: Segment): Promise<Channel> {
  const userId = segment.id;
  if (!userId) throw error(404, 'Channel not found');

  const account = await accountState(userId).catch(() => null);
  const canonical = canonicalLogin(account);
  if (canonical) throw redirect(308, `/user/${canonical}`);
  return { userId, channelName: `channel ${userId}` };
}

async function resolveChannel(segment: Segment): Promise<Channel> {
  return (await channelFromLogin(segment)) ?? channelFromID(segment);
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
  requireHost(url, 'commands');

  if (DEMO) {
    const d = await import('$lib/server/demo-data');
    return {
      userId: '1',
      channelName: parseSegment(params.channel).login ?? 'demo',
      creatorCode: d.demoCreatorCode,
      commands: d.demoPublicCommands satisfies PublicCommand[],
      modules: d.demoPublicModules satisfies PublicModule[],
      degraded: false
    };
  }

  const { userId, channelName } = await resolveChannel(parseSegment(params.channel));

    // Same message as an unknown channel, so a probe cannot tell hidden from absent.
  if (!(await userCommandsPage(userId).catch(() => true))) {
    locals.edgeCache404 = true;
    throw error(404, 'Channel not found');
  }

  try {
    const [commands, modules, account] = await Promise.all([
      listCommands(userId),
      listModules(userId),
      accountState(userId).catch(() => null)
    ]);

    return {
      userId,
      channelName: channelLabel(account, channelName),
      creatorCode: account?.creatorCode ?? null,
      commands: publicCommands(commands, locals.locale),
      modules: publicModules(modules, locals.locale),
      degraded: false
    };
  } catch {
    return {
      userId,
      channelName,
      creatorCode: null,
      commands: [],
      modules: [],
      degraded: true
    };
  }
};
