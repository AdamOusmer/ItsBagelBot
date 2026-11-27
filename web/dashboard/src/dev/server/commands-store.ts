// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MODULE_CATALOG, type CommandView } from '@bagel/kit';
import type { CommandInput, ModuleConfigPatch, ModulePatch, ModuleView } from '#lib/server/commands-store';
import { demoCommandRows, demoModuleConfigs } from '../fixtures';

export * from '#lib/server/commands-store';

type UserId = string;
type ModuleName = string;
type CommandName = string;

let commands: CommandView[] = structuredClone(demoCommandRows);

const seeded = demoModuleConfigs();
const modules = new Map<string, ModuleView>(
  MODULE_CATALOG.map((def) => [def.id, { name: def.id, is_enabled: true, configs: seeded[def.id] ?? {}, revision: 1 }])
);

function moduleRows(): ModuleView[] {
  return structuredClone([...modules.values()]);
}

function writeModule(name: ModuleName, isEnabled: boolean, configs: Record<string, unknown>): ModuleView {
  const revision = (modules.get(name)?.revision ?? 0) + 1;
  const row = { name, is_enabled: isEnabled, configs, revision };
  modules.set(name, row);
  return row;
}

function currentConfigs(name: ModuleName): Record<string, unknown> {
  return (modules.get(name)?.configs ?? {}) as Record<string, unknown>;
}

export async function listCommands(): Promise<CommandView[]> {
  return structuredClone(commands);
}

export async function listModules(): Promise<ModuleView[]> {
  return moduleRows();
}

export async function listModulesForEditing(): Promise<ModuleView[]> {
  return moduleRows();
}

export async function replaceProjectedModules(): Promise<boolean> {
  return true;
}

export async function upsertModule(_userId: UserId, name: ModuleName, isEnabled: boolean, configs?: unknown) {
  writeModule(name, isEnabled, (configs ?? {}) as Record<string, unknown>);
  return { modules: moduleRows(), synced: true };
}

export async function patchModule(p: ModulePatch): Promise<{ rev: number; conflict: boolean; synced: boolean }> {
  const row = writeModule(p.name, p.isEnabled, { ...currentConfigs(p.name), ...p.partial });
  return { rev: row.revision ?? 1, conflict: false, synced: true };
}

export async function patchModuleConfig(p: ModuleConfigPatch): Promise<void> {
  writeModule(p.name, modules.get(p.name)?.is_enabled ?? false, { ...currentConfigs(p.name), ...p.partial });
}

function commandView(cmd: CommandInput, previous: CommandView | undefined): CommandView {
  return {
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: cmd.isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    user_cooldown: cmd.userCooldown,
    allowed_user_id: cmd.allowedUserId,
    bump_counter: cmd.bumpCounter,
    uses: previous?.uses ?? '0'
  };
}

export async function upsertCommand(_userId: UserId, cmd: CommandInput, originalName?: CommandName) {
  const previous = commands.find((c) => c.name === (originalName ?? cmd.name));
  const kept = commands.filter((c) => c.name !== originalName && c.name !== cmd.name);
  commands = [...kept, commandView(cmd, previous)];
  return { commands: structuredClone(commands), restored: false };
}

export async function deleteCommand(_userId: UserId, name: CommandName) {
  commands = commands.filter((c) => c.name !== name);
  return { commands: structuredClone(commands) };
}
