// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { BUILTIN_COMMANDS } from './catalog/builtin-commands';
import { MODULE_CATALOG } from './catalog';

export type FeaturePreset = 'start-new' | 'integrate' | 'quiet' | 'import' | 'songs';
export type FeatureChange = { name: string; enabled: boolean };
export type FeatureRow = { name: string; is_enabled: boolean };

// Only explicit state differences need a write. A missing row uses the same
// shipped default as the dashboard and sesame, including default-on modules.
export function featurePresetChanges(preset: FeaturePreset, rows: readonly FeatureRow[]): FeatureChange[] {
  // Import applies its own configuration after the wizard is complete.
  if (preset === 'import') return [];

  const byName = new Map(rows.map((row) => [row.name, row]));
  const choices = [
    ...MODULE_CATALOG.filter((def) => preset !== 'integrate' && def.toggleable !== false && !def.hidden).map((def) => ({
      name: def.id,
      current: byName.get(def.id)?.is_enabled ?? def.defaultEnabled,
      enabled: preset === 'start-new' ? def.defaultEnabled : preset === 'songs' && def.id === 'songqueue'
    })),
    ...BUILTIN_COMMANDS.filter((def) => preset !== 'integrate' || def.id !== 'clip').map((def) => ({
      name: def.id,
      current: byName.get(def.id)?.is_enabled ?? def.defaultActive,
      enabled: preset === 'start-new' ? def.defaultActive : false
    }))
  ];
  return choices.filter((choice) => choice.current !== choice.enabled).map(({ name, enabled }) => ({ name, enabled }));
}
