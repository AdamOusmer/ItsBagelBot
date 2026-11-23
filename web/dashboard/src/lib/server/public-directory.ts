// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  BUILTIN_COMMANDS,
  commandHeadTrigger,
  DISABLED_TRIGGERS_KEY,
  MODULE_CATALOG,
  moduleCommandTriggerMap,
  normalizeTrigger,
  parseDisabledTriggers,
  PERM_LABELS,
  PERMS,
  tModuleCommandSummary,
  tModuleLabel,
  tModuleReplyPart,
  tModuleTagline,
  translate,
  type CommandView,
  type Locale,
  type ModuleCommandInfo,
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

type CatalogContext = {
  def: (typeof MODULE_CATALOG)[number];
  config: Record<string, string>;
  locale: Locale;
  t: (key: string) => string;
  disabled: Set<string>;
  triggerMap: Map<string, string[]>;
  headEnabled: Map<string, boolean>;
};

function swapLeadingTrigger(command: ModuleCommandInfo, active: readonly string[]): string {
  return command.trigger.replace(/^!?\S+/, `!${active[0]}`);
}

function commandPerm(ctx: CatalogContext, command: ModuleCommandInfo): string {
  const fallback = command.perm ?? 'everyone';
  const perm = command.permKey ? parsePerm(ctx.config[command.permKey], fallback) : fallback;
  return permLabel(ctx.locale, perm);
}

function resolveOwnedTrigger(
  ctx: CatalogContext,
  command: ModuleCommandInfo,
  owned: string[]
): { label: string; aliases: string[] } | null {
  const active = owned.filter((tr) => !ctx.disabled.has(tr));
  ctx.headEnabled.set(owned[0], active.length > 0);
  if (active.length === 0) return null;
  const label = active[0] === owned[0] ? command.trigger : swapLeadingTrigger(command, active);
  return { label, aliases: active.slice(1).map((a) => `!${a}`) };
}

function resolveTriggerDisplay(
  ctx: CatalogContext,
  command: ModuleCommandInfo
): { label: string; aliases: string[] } | null {
  const owned = ctx.triggerMap.get(command.trigger) ?? [];
  if (owned.length > 0) return resolveOwnedTrigger(ctx, command, owned);
  if (ctx.headEnabled.get(commandHeadTrigger(command)) === false) return null;
  return { label: command.trigger, aliases: [] };
}

function resolveCommandRow(ctx: CatalogContext, command: ModuleCommandInfo): ModuleCommand | null {
  if (!activeReply(ctx.config, command.enableKey)) return null;
  const display = resolveTriggerDisplay(ctx, command);
  if (!display) return null;
  const row = moduleCommand(display.label, tModuleCommandSummary(ctx.t, ctx.def.id, command), commandPerm(ctx, command));
  return { ...row, aliases: display.aliases };
}

function replyDisabled(ctx: CatalogContext, reply: (typeof MODULE_CATALOG)[number]['replies'][number]): boolean {
  if (!reply.command) return true;
  return ctx.disabled.has(normalizeTrigger(reply.command));
}

function replyCommandRow(
  ctx: CatalogContext,
  reply: (typeof MODULE_CATALOG)[number]['replies'][number],
  seen: Set<string>
): ModuleCommand | null {
  if (replyDisabled(ctx, reply) || !activeReply(ctx.config, reply.enableKey)) return null;
  const base = `!${reply.command}`;
  const label = tModuleReplyPart(ctx.t, ctx.def.id, reply, 'label');
  const trigger = label.toLowerCase().startsWith(base.toLowerCase()) ? label : base;
  if (seen.has(triggerBase(base)) || seen.has(triggerBase(trigger))) return null;
  seen.add(triggerBase(base));
  seen.add(triggerBase(trigger));
  return moduleCommand(trigger, tModuleReplyPart(ctx.t, ctx.def.id, reply, 'tagline'), permLabel(ctx.locale, 'everyone'));
}

// One row per chat command: the documented commands first, then any command
// only a reply names. Replies are the chat lines a command can answer with, so
// several share one trigger and must never each become a row.
function catalogCommands(def: (typeof MODULE_CATALOG)[number], config: Record<string, string>, locale: Locale): ModuleCommand[] {
  const ctx: CatalogContext = {
    def,
    config,
    locale,
    t: (key: string) => translate(locale, key),
    disabled: parseDisabledTriggers(config[DISABLED_TRIGGERS_KEY]),
    triggerMap: moduleCommandTriggerMap(def),
    headEnabled: new Map()
  };
  const commands = def.commands ?? [];
  const rows: ModuleCommand[] = [];
  for (const command of commands) {
    const row = resolveCommandRow(ctx, command);
    if (row) rows.push(row);
  }
  const seen = new Set(commands.map((command) => triggerBase(command.trigger)));
  for (const reply of def.replies) {
    const row = replyCommandRow(ctx, reply, seen);
    if (row) rows.push(row);
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
