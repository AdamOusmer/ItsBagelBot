// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { featurePresetChanges, type FeaturePreset, type FeatureChange } from '@bagel/kit';
import { listModules } from './commands-store';
import { setModuleEnabled } from './module-blob';

export async function applyFeaturePreset(userId: string, preset: FeaturePreset): Promise<FeatureChange[]> {
  if (preset === 'import') return [];
  const changes = featurePresetChanges(preset, await listModules(userId));
  // Sequential: upsertModule pushes the whole module list, so concurrent writes drop toggles.
  for (const change of changes) {
    await setModuleEnabled(userId, change.name, change.enabled);
  }
  return changes;
}
