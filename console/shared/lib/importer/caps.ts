// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Per-collection import caps, applied client-side after a local parse. The
// numbers live in @bagel/shared (IMPORT_ITEM_CAPS) mirrored from
// app/importer/mapping/validate.go so browser and server truncate/flag at the
// same boundary; this module is the browser-side enforcement point.

import type { ImportDiagnostic, ImportManifest } from './types';
import { IMPORT_ITEM_CAPS } from './types';

type CappedKind = keyof typeof IMPORT_ITEM_CAPS;

const KINDS: readonly CappedKind[] = ['commands', 'timers', 'triggers', 'quotes', 'counters'];

// applyImportCaps truncates every manifest collection to its cap in place,
// returning the same manifest plus one manifest-level warn diagnostic per
// truncated collection. Truncation — not pass-through-with-server-flags — is
// deliberate: it bounds both the POST body (the whole point of client-side
// parsing) and the review DOM, which cannot render tens of thousands of rows
// anyway. The server's own cap diagnostics stay authoritative for direct RPC
// callers; here they can never fire because the overflow was already cut.
interface CapSpec {
  code: string;
  label: string;
  cap: number;
}

// capRows bounds one section, emitting the standard truncation warn when rows
// fall past the cap. null means the section was absent/empty (stays omitted).
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
  // fetches ride the commands cap rather than getting a number of its own:
  // every definition serves a command slot in this same manifest, so the
  // commands ceiling bounds it by construction, and a second public cap would
  // invite drift from IMPORT_ITEM_CAPS (the numbers are mirrored server-side).
  // Parsers already refuse synthesis past this exact value; the slice only
  // matters for hand-built or older manifests POSTed directly.
  const fetches = capRows(
    manifest.fetches,
    { code: 'manifest_fetches_capped', label: 'fetch definitions', cap: IMPORT_ITEM_CAPS.commands },
    diagnostics
  );
  if (fetches) out.fetches = fetches as ImportManifest['fetches'];
  return { manifest: out, diagnostics };
}
