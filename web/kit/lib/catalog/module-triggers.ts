// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleCommandInfo, ModuleDef } from './module-def';

export const DISABLED_TRIGGERS_KEY = 'disabledTriggers';

export function normalizeTrigger(raw: string): string {
  const stripped = raw.trim().replace(/^!/, '');
  const [head = ''] = stripped.split(/\s+/, 1);
  return head.toLowerCase();
}

export function commandHeadTrigger(command: Pick<ModuleCommandInfo, 'trigger'>): string {
  return normalizeTrigger(command.trigger);
}

export function parseDisabledTriggers(raw: string | undefined): Set<string> {
  if (!raw) return new Set();
  const out = new Set<string>();
  for (const part of raw.split(',')) {
    const name = normalizeTrigger(part);
    if (name) out.add(name);
  }
  return out;
}

function allowedTrigger(name: string, allowed?: ReadonlySet<string>): boolean {
  if (!name) return false;
  return !allowed || allowed.has(name);
}

export function formatDisabledTriggers(disabled: Iterable<string>, allowed?: ReadonlySet<string>): string {
  const out = new Set<string>();
  for (const raw of disabled) {
    const name = normalizeTrigger(raw);
    if (allowedTrigger(name, allowed)) out.add(name);
  }
  return [...out].sort().join(',');
}

function applyTriggerState(disabled: Set<string>, raw: string, enabled: boolean): void {
  const name = normalizeTrigger(raw);
  if (!name) return;
  if (enabled) disabled.delete(name);
  else disabled.add(name);
}

export function toggleTriggersInList(
  raw: string | undefined,
  triggers: readonly string[],
  enabled: boolean,
  allowed?: ReadonlySet<string>
): string {
  const disabled = parseDisabledTriggers(raw);
  for (const item of triggers) applyTriggerState(disabled, item, enabled);
  return formatDisabledTriggers(disabled, allowed);
}

function claimCommandTriggers(command: ModuleCommandInfo, seen: Set<string>): string[] {
  const head = commandHeadTrigger(command);
  if (!head || seen.has(head)) return [];
  const owned: string[] = [];
  for (const raw of [head, ...(command.aliases ?? [])]) {
    const name = normalizeTrigger(raw);
    if (!name || seen.has(name)) continue;
    seen.add(name);
    owned.push(name);
  }
  return owned;
}

export function moduleCommandTriggerMap(def: ModuleDef): Map<string, string[]> {
  const map = new Map<string, string[]>();
  const seen = new Set<string>();
  for (const command of def.commands ?? []) {
    map.set(command.trigger, claimCommandTriggers(command, seen));
  }
  return map;
}

export function moduleAllowedTriggers(def: ModuleDef): Set<string> {
  if (def.toggleable === false) return new Set();
  const allowed = new Set<string>();
  for (const triggers of moduleCommandTriggerMap(def).values()) {
    for (const name of triggers) allowed.add(name);
  }
  return allowed;
}
