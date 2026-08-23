// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Per-collection import caps, applied client-side after a local parse. The
// numbers live in @bagel/shared (IMPORT_ITEM_CAPS) mirrored from
// app/importer/mapping/validate.go so browser and server truncate/flag at the
// same boundary; this module is the browser-side enforcement point.

import type { ImportDiagnostic, ImportManifest } from '../types';
import { IMPORT_ITEM_CAPS } from '../types';

type CappedKind = keyof typeof IMPORT_ITEM_CAPS;

const KINDS: readonly CappedKind[] = ['commands', 'timers', 'triggers', 'quotes', 'counters'];

// applyImportCaps truncates every manifest collection to its cap in place,
// returning the same manifest plus one manifest-level warn diagnostic per
// truncated collection. Truncation — not pass-through-with-server-flags — is
// deliberate: it bounds both the POST body (the whole point of client-side
// parsing) and the review DOM, which cannot render tens of thousands of rows
// anyway. The server's own cap diagnostics stay authoritative for direct RPC
// callers; here they can never fire because the overflow was already cut.
export function applyImportCaps(
  manifest: ImportManifest
): { manifest: ImportManifest; diagnostics: ImportDiagnostic[] } {
  const diagnostics: ImportDiagnostic[] = [];
  const out: ImportManifest = {};
  if (manifest.automod) out.automod = manifest.automod;
  for (const kind of KINDS) {
    const rows = manifest[kind];
    if (!rows?.length) continue;
    const cap = IMPORT_ITEM_CAPS[kind];
    (out[kind] as unknown[]) = rows.slice(0, cap);
    if (rows.length > cap) {
      diagnostics.push({
        severity: 'warn',
        item_index: -1,
        code: `manifest_${kind}_capped`,
        message: `${rows.length - cap} ${kind} dropped past the ${cap}-item import limit`
      });
    }
  }
  return { manifest: out, diagnostics };
}
