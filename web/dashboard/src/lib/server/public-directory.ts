// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  BUILTIN_COMMANDS,
  MODULE_CATALOG,
  PERM_LABELS,
  PERMS,
  tModuleCommandSummary,
  tModuleLabel,
  tModuleReplyPart,
  tModuleTagline,
  translate,
  type CommandView,
  type Locale,
  type Perm
} from '@bagel/kit';
import { usesCount } from '@bagel/kit/uses';
import { formatCounterValue } from '@bagel/kit/validation';
import type { ModuleView } from '#lib/server/commands-store';

export type PublicCommand = {
  trigger: string;
  aliases: string[];
  response: string;
  perm: string;
  cooldown: number;
  userCooldown: number;
  liveOnly: boolean;
  uses: string;
};

export type ModuleDetail = {
  label: string;
  meta: string;
};

export type ModuleCommand = ModuleDetail & {
  perm: string;
  aliases: string[];
  cooldown: number;
  liveOnly: boolean;
};

export type PublicModule = {
  id: string;
  label: string;
  category: string;
  tagline: string;
  commands: ModuleCommand[];
  events: ModuleDetail[];
};

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

function parsePerm(raw: unknown, fallback: Perm): Perm {
  return PERMS.includes(raw as Perm) ? (raw as Perm) : fallback;
}

function permLabel(locale: Locale, perm: Perm): string {
  const permKey = `perm.${perm}`;
  const localized = translate(locale, permKey);
  return localized === permKey ? (PERM_LABELS[perm] ?? PERM_LABELS.everyone) : localized;
}

export function publicCommands(rows: CommandView[], locale: Locale = 'en'): PublicCommand[] {
  const builtinNames = new Set(BUILTIN_COMMANDS.map((cmd) => cmd.id));
  return rows
    .filter((cmd) => cmd.is_active && cmd.name && !builtinNames.has(cmd.name))
    .map((cmd) => {
      return {
        trigger: `!${cmd.name}`,
        aliases: (cmd.aliases ?? []).filter(Boolean).map((alias) => `!${alias}`),
        response: cmd.response,
        perm: permLabel(locale, parsePerm(cmd.perm, 'everyone')),
        cooldown: Math.max(0, Number(cmd.cooldown ?? 0) || 0),
        userCooldown: Math.max(0, Number(cmd.user_cooldown ?? 0) || 0),
        liveOnly: cmd.stream_online_only === true,
        uses: cmd.uses == null ? '' : formatCounterValue(usesCount(cmd).toString(), locale)
      };
    })
    .sort((a, b) => a.trigger.localeCompare(b.trigger));
}

function activeModule(byName: Map<string, ModuleView>, id: string, fallback: boolean): boolean {
  const row = byName.get(id);
  return row ? row.is_enabled : fallback;
}

// Strips argument placeholders so "!raffle draw [winners]" covers replies to "!raffle draw".
function triggerBase(trigger: string): string {
  return trigger
    .split(/\s+/)
    .filter((word) => !/^[<[]/.test(word))
    .join(' ')
    .toLowerCase();
}

function moduleCommand(label: string, meta: string, perm: string): ModuleCommand {
  return { label, meta, perm, aliases: [], cooldown: 0, liveOnly: false };
}

// One row per chat command: the documented commands first, then any command
// only a reply names. Replies are the chat lines a command can answer with, so
// several share one trigger and must never each become a row.
function catalogCommands(def: (typeof MODULE_CATALOG)[number], config: Record<string, string>, locale: Locale): ModuleCommand[] {
  const t = (key: string) => translate(locale, key);
  const commands = (def.commands ?? []).filter((command) => !command.requiresSetting || config[command.requiresSetting] === 'on');
  const rows = commands.map((command) => {
    const fallback = command.perm ?? 'everyone';
    const perm = command.permKey ? parsePerm(config[command.permKey], fallback) : fallback;
    return moduleCommand(command.trigger, tModuleCommandSummary(t, def.id, command), permLabel(locale, perm));
  });
  const seen = new Set((def.commands ?? []).map((command) => triggerBase(command.trigger)));
  for (const reply of def.replies) {
    if (!reply.command || !activeReply(config, reply.enableKey)) continue;
    const base = `!${reply.command}`;
    const label = tModuleReplyPart(t, def.id, reply, 'label');
    const trigger = label.toLowerCase().startsWith(base.toLowerCase()) ? label : base;
    if (seen.has(triggerBase(base)) || seen.has(triggerBase(trigger))) continue;
    seen.add(triggerBase(base));
    seen.add(triggerBase(trigger));
    rows.push(moduleCommand(trigger, tModuleReplyPart(t, def.id, reply, 'tagline'), permLabel(locale, 'everyone')));
  }
  return rows;
}

function catalogEntry(def: (typeof MODULE_CATALOG)[number], byName: Map<string, ModuleView>, locale: Locale): PublicModule[] {
  if (!activeModule(byName, def.id, def.defaultEnabled)) return [];
  const config = asConfig(byName.get(def.id)?.configs);
  const live = def.replies.filter((reply) => activeReply(config, reply.enableKey));
  return [
    {
      id: def.id,
      label: tModuleLabel((key) => translate(locale, key), def),
      category: 'Module',
      tagline: tModuleTagline((key) => translate(locale, key), def),
      commands: catalogCommands(def, config, locale),
      events: live
        .filter((reply) => !reply.command)
        .map((reply) => ({
          label: tModuleReplyPart((key) => translate(locale, key), def.id, reply, 'label'),
          meta: tModuleReplyPart((key) => translate(locale, key), def.id, reply, 'event')
        }))
    }
  ];
}

function localizedBuiltin(locale: Locale, id: string, part: 'label' | 'summary', fallback: string): string {
  const key = `builtinDirectory.${id}.${part}`;
  const value = translate(locale, key);
  return value === key ? fallback : value;
}

function builtinEntry(def: (typeof BUILTIN_COMMANDS)[number], byName: Map<string, ModuleView>, locale: Locale): PublicModule[] {
  if (!activeModule(byName, def.id, def.defaultActive)) return [];
  const config = asConfig(byName.get(def.id)?.configs);
  return [
    {
      id: def.id,
      label: localizedBuiltin(locale, def.id, 'label', def.label),
      category: 'Built-in',
      tagline: localizedBuiltin(locale, def.id, 'summary', def.summary),
      commands: [
        {
          label: `!${def.id}`,
          meta: localizedBuiltin(locale, def.id, 'summary', def.summary),
          perm: permLabel(locale, parsePerm(config.permission, def.defaultPerm)),
          aliases: (def.aliases ?? []).filter(Boolean).map((alias) => `!${alias}`),
          cooldown: def.defaultCooldown,
          liveOnly: def.liveOnly
        }
      ],
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
