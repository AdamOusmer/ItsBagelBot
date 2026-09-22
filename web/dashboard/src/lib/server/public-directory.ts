// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Shared shaping for the public, unauthenticated channel pages: every surface
// that lists a channel's !commands (the /user/<channel> page and the
// leaderboard host page) must present them identically, so the filtering and
// labeling live here once.

import {
  BUILTIN_COMMANDS,
  MODULE_CATALOG,
  PERM_LABELS,
  tModuleLabel,
  tModuleReplyPart,
  tModuleTagline,
  translate,
  translateList,
  type CommandView,
  type Locale,
  type Perm
} from '@bagel/kit';
import type { ModuleView } from '$lib/server/commands-store';

export type PublicCommand = {
  trigger: string;
  aliases: string[];
  response: string;
  perm: string;
  cooldown: number;
  liveOnly: boolean;
  uses: string;
};

export type ModuleDetail = {
  label: string;
  meta: string;
};

export type PublicModule = {
  id: string;
  label: string;
  category: string;
  tagline: string;
  commands: ModuleDetail[];
  events: ModuleDetail[];
};

/**
 * The name a public page labels a channel with: the Twitch display name (the
 * login in the owner's casing) when the users service holds one, else the
 * stored login, else the caller's fallback (the URL segment, or a placeholder
 * for an id-only lookup). One place, so the commands page and the leaderboard
 * cannot drift on which name wins.
 */
export function channelLabel(
  record: { displayName?: string; username?: string } | null | undefined,
  fallback: string
): string {
  return record?.displayName || record?.username || fallback;
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

/** The channel's custom !commands as the public pages show them. */
export function publicCommands(rows: CommandView[], locale: Locale = 'en'): PublicCommand[] {
  const builtinNames = new Set(BUILTIN_COMMANDS.map((cmd) => cmd.id));
  return rows
    .filter((cmd) => cmd.is_active && cmd.name && !builtinNames.has(cmd.name))
    .map((cmd) => {
      const perm = (cmd.perm ?? 'everyone') as Perm;
      const permKey = `perm.${perm}`;
      const localizedPerm = translate(locale, permKey);
      return {
        trigger: `!${cmd.name}`,
        aliases: (cmd.aliases ?? []).filter(Boolean).map((alias) => `!${alias}`),
        response: cmd.response,
        perm: localizedPerm === permKey ? (PERM_LABELS[perm] ?? PERM_LABELS.everyone) : localizedPerm,
        cooldown: Math.max(0, Number(cmd.cooldown ?? 0) || 0),
        liveOnly: cmd.stream_online_only === true,
        uses: cmd.uses == null ? '' : String(cmd.uses)
      };
    })
    .sort((a, b) => a.trigger.localeCompare(b.trigger));
}

/** The channel's active modules and built-ins with their chat surfaces. */
// activeModule resolves a module's on/off state: the stored row wins, and a
// module the broadcaster never touched falls back to its catalog default.
function activeModule(byName: Map<string, ModuleView>, id: string, fallback: boolean): boolean {
  const row = byName.get(id);
  return row ? row.is_enabled : fallback;
}

// catalogEntry shapes one catalog module, splitting its replies into the
// commands a viewer can type and the events that fire on their own.
function catalogEntry(def: (typeof MODULE_CATALOG)[number], byName: Map<string, ModuleView>, locale: Locale): PublicModule[] {
  if (!activeModule(byName, def.id, def.defaultEnabled)) return [];
  const config = asConfig(byName.get(def.id)?.configs);
  const live = def.replies.filter((reply) => activeReply(config, reply.enableKey));
  return [
    {
      id: def.id,
      label: tModuleLabel((key) => translate(locale, key), def),
      // Catalog modules share one bucket; built-ins get their own 'Built-in'
      // category below. ModuleDef itself carries no category field.
      category: 'Module',
      tagline: tModuleTagline((key) => translate(locale, key), def),
      commands: live
        .filter((reply) => reply.command)
        .map((reply) => ({ label: `!${reply.command}`, meta: tModuleReplyPart((key) => translate(locale, key), def.id, reply, 'tagline') })),
      events: live
        .filter((reply) => !reply.command)
        .map((reply) => ({
          label: tModuleReplyPart((key) => translate(locale, key), def.id, reply, 'label'),
          meta: tModuleReplyPart((key) => translate(locale, key), def.id, reply, 'event')
        }))
    }
  ];
}

// builtinEntry shapes one built-in command, which is always a single command
// and never carries events.
function localizedBuiltin(locale: Locale, id: string, part: 'label' | 'summary', fallback: string): string {
  const key = `builtinDirectory.${id}.${part}`;
  const value = translate(locale, key);
  return value === key ? fallback : value;
}

function localizedBuiltinUsage(locale: Locale, id: string, fallback: string[]): string {
  const key = `builtinDirectory.${id}.usage`;
  const value = translateList(locale, key);
  return value.length ? value.join(' / ') : fallback.join(' / ');
}

function builtinEntry(def: (typeof BUILTIN_COMMANDS)[number], byName: Map<string, ModuleView>, locale: Locale): PublicModule[] {
  if (!activeModule(byName, def.id, def.defaultActive)) return [];
  return [
    {
      id: def.id,
      label: localizedBuiltin(locale, def.id, 'label', def.label),
      category: 'Built-in',
      tagline: localizedBuiltin(locale, def.id, 'summary', def.summary),
      commands: [{ label: `!${def.id}`, meta: localizedBuiltinUsage(locale, def.id, def.usage) }],
      events: []
    }
  ];
}

export function publicModules(rows: ModuleView[], locale: Locale = 'en'): PublicModule[] {
  const byName = new Map(rows.map((row) => [row.name, row]));
  const catalog = MODULE_CATALOG.filter((def) => !def.hidden && def.toggleable !== false);
  return [
    ...catalog.flatMap((def) => catalogEntry(def, byName, locale)),
    ...BUILTIN_COMMANDS.flatMap((def) => builtinEntry(def, byName, locale))
  ].sort((a, b) => a.category.localeCompare(b.category) || a.label.localeCompare(b.label));
}
