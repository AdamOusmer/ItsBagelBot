// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Role presets change only shipped, toggleable features. They keep each stored
// config blob intact, so a channel can turn a feature back on later without
// rebuilding its settings. Custom commands are separate user-authored content.
import { featurePresetChanges, type FeaturePreset, type FeatureChange } from '@bagel/kit';
import { listModules } from './commands-store';
import { setModuleEnabled } from './module-blob';

export async function applyFeaturePreset(userId: string, preset: FeaturePreset): Promise<FeatureChange[]> {
  if (preset === 'import') return [];
  const changes = featurePresetChanges(preset, await listModules(userId));
  // upsertModule pushes a complete projected module list after each write.
  // Await in order so two writes cannot race and drop one another's toggle.
  for (const change of changes) {
    await setModuleEnabled(userId, change.name, change.enabled);
  }
  return changes;
}
