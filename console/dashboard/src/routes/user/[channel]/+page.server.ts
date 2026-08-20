// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import {
  BUILTIN_COMMANDS,
  MODULE_CATALOG,
  PERM_LABELS,
  type CommandView,
  type Perm
} from '@bagel/shared';
import { listCommands, listModules, type ModuleView } from '$lib/server/commands-store';
import { accountState, resolveLogin } from '$lib/server/services';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// SSR renders the full page for SEO/no-JS; hydration is left on so the hero's
// warm light-field ("star" motes) and decode-on-view title can animate. Both
// degrade to a static header when JS is off or reduced-motion is set.

type PublicCommand = {
  trigger: string;
  aliases: string[];
  response: string;
  perm: string;
  cooldown: number;
  liveOnly: boolean;
  uses: string;
};

type ModuleDetail = {
  label: string;
  meta: string;
};

type PublicModule = {
  id: string;
  label: string;
  category: string;
  tagline: string;
  commands: ModuleDetail[];
  events: ModuleDetail[];
};

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

function asConfig(raw: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (!raw || typeof raw !== 'object') return out;
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    out[key] = value == null ? '' : String(value);
  }
  return out;
}

function enabledFlag(value: string | undefined): boolean {
  return value !== 'off';
}

function activeReply(config: Record<string, string>, enableKey?: string): boolean {
  return !enableKey || enabledFlag(config[enableKey]);
}

function publicCommands(rows: CommandView[]): PublicCommand[] {
  const builtinNames = new Set(BUILTIN_COMMANDS.map((cmd) => cmd.id));
  return rows
    .filter((cmd) => cmd.is_active && cmd.name && !builtinNames.has(cmd.name))
    .map((cmd) => {
      const perm = (cmd.perm ?? 'everyone') as Perm;
      return {
        trigger: `!${cmd.name}`,
        aliases: (cmd.aliases ?? []).filter(Boolean).map((alias) => `!${alias}`),
        response: cmd.response,
        perm: PERM_LABELS[perm] ?? PERM_LABELS.everyone,
        cooldown: Math.max(0, Number(cmd.cooldown ?? 0) || 0),
        liveOnly: cmd.stream_online_only === true,
        uses: cmd.uses == null ? '' : String(cmd.uses)
      };
    })
    .sort((a, b) => a.trigger.localeCompare(b.trigger));
}

function publicModules(rows: ModuleView[]): PublicModule[] {
  const byName = new Map(rows.map((row) => [row.name, row]));

  const catalogModules = MODULE_CATALOG.filter((def) => !def.hidden && def.toggleable !== false).flatMap((def): PublicModule[] => {
    const row = byName.get(def.id);
    const active = row ? row.is_enabled : def.defaultEnabled;
    if (!active) return [];

    const config = asConfig(row?.configs);
    const commands = def.replies
      .filter((reply) => reply.command && activeReply(config, reply.enableKey))
      .map((reply) => ({
        label: `!${reply.command}`,
        meta: reply.tagline
      }));
    const events = def.replies
      .filter((reply) => !reply.command && activeReply(config, reply.enableKey))
      .map((reply) => ({
        label: reply.label,
        meta: reply.event
      }));

    return [{
      id: def.id,
      label: def.label,
      // Catalog modules share one bucket; built-ins get their own 'Built-in'
      // category below. ModuleDef itself carries no category field.
      category: 'Module',
      tagline: def.tagline,
      commands,
      events
    }];
  });

  const builtinModules = BUILTIN_COMMANDS.flatMap((def): PublicModule[] => {
    const row = byName.get(def.id);
    const active = row ? row.is_enabled : def.defaultActive;
    if (!active) return [];
    return [{
      id: def.id,
      label: def.label,
      category: 'Built-in',
      tagline: def.summary,
      commands: [{
        label: `!${def.id}`,
        meta: def.usage.join(' / ')
      }],
      events: []
    }];
  });

  return [...catalogModules, ...builtinModules].sort((a, b) => {
    const byCategory = a.category.localeCompare(b.category);
    return byCategory || a.label.localeCompare(b.label);
  });
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

export const load: PageServerLoad = async ({ params }) => {
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
