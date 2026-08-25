// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Shared shaping for the public, unauthenticated channel pages: every surface
// that lists a channel's !commands (the /user/<channel> page and the
// leaderboard host page) must present them identically, so the filtering and
// labeling live here once.

import { BUILTIN_COMMANDS, MODULE_CATALOG, PERM_LABELS, type CommandView, type Perm } from '@bagel/shared';
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
export function publicCommands(rows: CommandView[]): PublicCommand[] {
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

/** The channel's active modules and built-ins with their chat surfaces. */
// activeModule resolves a module's on/off state: the stored row wins, and a
// module the broadcaster never touched falls back to its catalog default.
function activeModule(byName: Map<string, ModuleView>, id: string, fallback: boolean): boolean {
  const row = byName.get(id);
  return row ? row.is_enabled : fallback;
}

// catalogEntry shapes one catalog module, splitting its replies into the
// commands a viewer can type and the events that fire on their own.
function catalogEntry(def: (typeof MODULE_CATALOG)[number], byName: Map<string, ModuleView>): PublicModule[] {
  if (!activeModule(byName, def.id, def.defaultEnabled)) return [];
  const config = asConfig(byName.get(def.id)?.configs);
  const live = def.replies.filter((reply) => activeReply(config, reply.enableKey));
  return [
    {
      id: def.id,
      label: def.label,
      // Catalog modules share one bucket; built-ins get their own 'Built-in'
      // category below. ModuleDef itself carries no category field.
      category: 'Module',
      tagline: def.tagline,
      commands: live
        .filter((reply) => reply.command)
        .map((reply) => ({ label: `!${reply.command}`, meta: reply.tagline })),
      events: live
        .filter((reply) => !reply.command)
        .map((reply) => ({ label: reply.label, meta: reply.event }))
    }
  ];
}

// builtinEntry shapes one built-in command, which is always a single command
// and never carries events.
function builtinEntry(def: (typeof BUILTIN_COMMANDS)[number], byName: Map<string, ModuleView>): PublicModule[] {
  if (!activeModule(byName, def.id, def.defaultActive)) return [];
  return [
    {
      id: def.id,
      label: def.label,
      category: 'Built-in',
      tagline: def.summary,
      commands: [{ label: `!${def.id}`, meta: def.usage.join(' / ') }],
      events: []
    }
  ];
}

export function publicModules(rows: ModuleView[]): PublicModule[] {
  const byName = new Map(rows.map((row) => [row.name, row]));
  const catalog = MODULE_CATALOG.filter((def) => !def.hidden && def.toggleable !== false);
  return [
    ...catalog.flatMap((def) => catalogEntry(def, byName)),
    ...BUILTIN_COMMANDS.flatMap((def) => builtinEntry(def, byName))
  ].sort((a, b) => a.category.localeCompare(b.category) || a.label.localeCompare(b.label));
}
