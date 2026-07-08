import { error } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import {
  BUILTIN_COMMANDS,
  MODULE_CATALOG,
  PERM_LABELS,
  type CommandView,
  type Perm
} from '@bagel/shared';
import { listCommands, listModules, type ModuleView } from '$lib/server/commands-store';
import { accountState } from '$lib/server/services';
import { env } from '$env/dynamic/private';

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

const demoCommands: PublicCommand[] = [
  {
    trigger: '!bagel',
    aliases: ['!snack'],
    response: '{user} tosses a warm bagel to {target}. Toasty.',
    perm: PERM_LABELS.everyone,
    cooldown: 10,
    liveOnly: false,
    uses: '1.2k'
  },
  {
    trigger: '!socials',
    aliases: ['!links'],
    response: 'Follow along on Twitch and everywhere else.',
    perm: PERM_LABELS.everyone,
    cooldown: 30,
    liveOnly: false,
    uses: '288'
  }
];

const demoModules: PublicModule[] = [
  {
    id: 'clip',
    label: 'Clip',
    category: 'Built-in',
    tagline: 'Let viewers capture and share a recent stream moment.',
    commands: [{ label: '!clip', meta: 'clip the last moment' }],
    events: []
  }
];

function cleanChannel(raw: string | null): string {
  // The bot passes the broadcaster's Twitch display name here, which may carry
  // non-ASCII letters (localized/CJK handles). Keep unicode letters, numbers and
  // underscore; drop spaces, punctuation and anything HTML-dangerous, then cap.
  return (raw ?? '').replace(/^@+/, '').replace(/[^\p{L}\p{N}_]/gu, '').slice(0, 32);
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

  const catalogModules = MODULE_CATALOG.flatMap((def): PublicModule[] => {
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

export const load: PageServerLoad = async ({ params, url }) => {
  const userId = params.userId;
  if (!/^[1-9]\d{0,19}$/.test(userId)) {
    throw error(404, 'Channel not found');
  }

  const channel = cleanChannel(url.searchParams.get('channel'));
  const channelName = channel || `channel ${userId}`;

  if (env.DEMO === '1') {
    return {
      userId,
      channelName,
      creatorCode: 'MAVEY10',
      commands: demoCommands,
      modules: demoModules,
      degraded: false
    };
  }

  try {
    const [commands, modules, account] = await Promise.all([
      listCommands(userId),
      listModules(userId),
      accountState(userId).catch(() => null)
    ]);

    return {
      userId,
      channelName,
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
