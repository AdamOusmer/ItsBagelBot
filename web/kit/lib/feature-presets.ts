// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { BUILTIN_COMMANDS } from './catalog/builtin-commands';
import { MODULE_CATALOG } from './catalog';

export type FeaturePreset = 'start-new' | 'integrate' | 'quiet' | 'import' | 'songs';
export type FeatureChange = { name: string; enabled: boolean };
export type FeatureRow = { name: string; is_enabled: boolean };

type ModuleDef = (typeof MODULE_CATALOG)[number];
type CommandDef = (typeof BUILTIN_COMMANDS)[number];
type Choice = { name: string; shipped: boolean; enabled: boolean };

function moduleOffered(preset: FeaturePreset, def: ModuleDef): boolean {
  return preset !== 'integrate' && def.toggleable !== false && !def.hidden;
}

function moduleTarget(preset: FeaturePreset, def: ModuleDef): boolean {
  if (preset === 'start-new') return def.defaultEnabled;
  return preset === 'songs' && def.id === 'songqueue';
}

function commandOffered(preset: FeaturePreset, def: CommandDef): boolean {
  return preset !== 'integrate' || def.id !== 'clip';
}

function commandTarget(preset: FeaturePreset, def: CommandDef): boolean {
  return preset === 'start-new' && def.defaultActive;
}

function presetChoices(preset: FeaturePreset): Choice[] {
  const modules = MODULE_CATALOG.filter((def) => moduleOffered(preset, def)).map((def) => ({
    name: def.id,
    shipped: def.defaultEnabled,
    enabled: moduleTarget(preset, def)
  }));
  const commands = BUILTIN_COMMANDS.filter((def) => commandOffered(preset, def)).map((def) => ({
    name: def.id,
    shipped: def.defaultActive,
    enabled: commandTarget(preset, def)
  }));
  return [...modules, ...commands];
}

export function featurePresetChanges(preset: FeaturePreset, rows: readonly FeatureRow[]): FeatureChange[] {
  if (preset === 'import') return [];
  const current = new Map(rows.map((row) => [row.name, row.is_enabled]));
  return presetChoices(preset)
    .filter((choice) => (current.get(choice.name) ?? choice.shipped) !== choice.enabled)
    .map(({ name, enabled }) => ({ name, enabled }));
}
