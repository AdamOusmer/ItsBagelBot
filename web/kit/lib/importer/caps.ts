// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ImportDiagnostic, ImportManifest } from './types';
import { IMPORT_ITEM_CAPS } from './types';

type CappedKind = keyof typeof IMPORT_ITEM_CAPS;

const KINDS: readonly CappedKind[] = ['commands', 'timers', 'triggers', 'quotes'];

interface CapSpec {
  code: string;
  label: string;
  cap: number;
}

function capRows(
  rows: unknown[] | undefined,
  spec: CapSpec,
  diagnostics: ImportDiagnostic[]
): unknown[] | null {
  if (!rows?.length) return null;
  if (rows.length > spec.cap) {
    diagnostics.push({
      severity: 'warn',
      item_index: -1,
      code: spec.code,
      message: `${rows.length - spec.cap} ${spec.label} dropped past the ${spec.cap}-item import limit`
    });
  }
  return rows.slice(0, spec.cap);
}

export function applyImportCaps(
  manifest: ImportManifest
): { manifest: ImportManifest; diagnostics: ImportDiagnostic[] } {
  const diagnostics: ImportDiagnostic[] = [];
  const out: ImportManifest = {};
  if (manifest.automod) out.automod = manifest.automod;
  for (const kind of KINDS) {
    const capped = capRows(
      manifest[kind],
      { code: `manifest_${kind}_capped`, label: kind, cap: IMPORT_ITEM_CAPS[kind] },
      diagnostics
    );
    if (capped) (out[kind] as unknown[]) = capped;
  }
  const fetches = capRows(
    manifest.fetches,
    { code: 'manifest_fetches_capped', label: 'fetch definitions', cap: IMPORT_ITEM_CAPS.commands },
    diagnostics
  );
  if (fetches) out.fetches = fetches as ImportManifest['fetches'];
  return { manifest: out, diagnostics };
}
