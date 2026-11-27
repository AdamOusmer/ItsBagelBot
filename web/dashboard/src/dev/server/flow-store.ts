// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AlertKey } from '@bagel/kit/automations';
import type { SaveFlowInput, SaveFlowResult, StoredFlow } from '#lib/server/flow-store';
import { demoExportRows, demoImport } from '../flow-fixtures';

export * from '#lib/server/flow-store';

export async function listFlows(): Promise<StoredFlow[]> {
  return demoExportRows().map((row) => ({
    ref: { layer: 'automation', id: row.id ?? row.name },
    name: row.name,
    enabled: true,
    revision: '1',
    definition: row.definition
  }));
}

export async function takenAlerts(): Promise<AlertKey[]> {
  return [];
}

export async function saveFlow(_locals: App.Locals, input: SaveFlowInput): Promise<SaveFlowResult> {
  const id = demoImport(input.name, input.definition);
  const flow: StoredFlow = { ref: { layer: 'automation', id }, name: input.name, enabled: false, revision: '1', definition: input.definition };
  return { ok: true, flow, warnings: [] };
}
