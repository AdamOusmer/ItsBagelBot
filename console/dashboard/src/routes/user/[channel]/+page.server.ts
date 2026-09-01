// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import { listCommands, listModules } from '$lib/server/commands-store';
import { publicCommands, publicModules, type PublicCommand, type PublicModule } from '$lib/server/public-directory';
import { accountState, resolveLogin } from '$lib/server/services';
import { requireHost } from '$lib/server/seo-hosts';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// SSR renders the full page for SEO/no-JS; hydration is left on so the hero's
// warm light-field ("star" motes) and decode-on-view title can animate. Both
// degrade to a static header when JS is off or reduced-motion is set.

// Twitch login shape: letters, digits and underscore, 25 max. Anything else
// cannot name a channel, so it is a 404 rather than a lookup.
const LOGIN_RE = /^[a-z0-9_]{1,25}$/;
const ID_RE = /^[1-9]\d{0,19}$/;

// Both readings of the URL segment, decided once. A segment can be both: Twitch
// allows all-numeric logins, so "12345678" is a candidate login and a candidate
// broadcaster id, and the login reading has to win or such a link would open
// the wrong channel.
type Segment = { login: string | null; id: string | null };

function parseSegment(raw: string): Segment {
  const cleaned = (raw ?? '').replace(/^@+/, '').toLowerCase();
  return {
    login: LOGIN_RE.test(cleaned) ? cleaned : null,
    id: ID_RE.test(raw) ? raw : null
  };
}

type Channel = { userId: string; channelName: string };

// One channel, one URL: the canonical form is the lower-cased login the users
// service stores. Returns null when the record carries nothing that can name a
// page, so a caller redirects only to somewhere real.
function canonicalLogin(record: { username?: string | null } | null): string | null {
  const login = (record?.username ?? '').toLowerCase();
  return LOGIN_RE.test(login) ? login : null;
}

// Login reading of the segment, which is the form every link takes now. Returns
// null when the segment is not login-shaped or resolves to nothing, so the id
// reading gets its turn.
async function channelFromLogin(segment: Segment): Promise<Channel | null> {
  const login = segment.login;
  if (!login) return null;

  const found = await resolveLogin(login).catch(() => null);
  if (!found?.userId) return null;

  const canonical = canonicalLogin(found) ?? login;
  if (canonical !== login) throw redirect(308, `/user/${canonical}`);
  return { userId: found.userId, channelName: found.username || login };
}

// Id reading, kept so links shared before the URL changed still open. The name
// is read from the users service, never from the URL or a query string, so a
// hand-edited link cannot relabel the channel.
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

// One channel, one URL — and one HOST.
//
// The canonical-form redirects above settle which SEGMENT names a channel;
// requireHost settles which ORIGIN serves it. traefik routes four hostnames to
// this app (deploy/k8s/console-dashboard.yaml) and the route table is the same
// on all of them, so /user/<login> answered 200 on every one. The leaderboard
// board page linked here relatively, which is how
// leaderboard.itsbagelbot.com/user/<login> came to serve the commands page
// under the wrong origin — one document at four URLs, and a visitor who clicked
// a channel name from a board landed on a page that looked like it had moved
// hosts on them.
//
// commands.itsbagelbot.com is the canonical host because it is the one the bot
// prints in chat: !cmd answers with <PublicBaseURL>/user/<login>, defaulting to
// that origin (app/sesame/modules/cmd.go).
export const load: PageServerLoad = async ({ params, url }) => {
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

  try {
    const [commands, modules, account] = await Promise.all([
      listCommands(userId),
      listModules(userId),
      accountState(userId).catch(() => null)
    ]);

    return {
      userId,
      channelName: account?.username || channelName,
      creatorCode: account?.creatorCode ?? null,
      commands: publicCommands(commands),
      modules: publicModules(modules),
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
