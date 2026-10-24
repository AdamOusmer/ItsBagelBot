// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CODE } from '@bagel/kit/importer/validate';
import type { ImportDiagnostic, ImportManifest, ImportSource, ImportStats, PreviewResponse } from '@bagel/kit';

export type ImportPreviewRequest = {
  source: ImportSource | '';
  credential?: string;
  file_b64?: string;
  manifest?: ImportManifest;
};

export function emptyStats(): ImportStats {
  return { commands: 0, timers: 0, triggers: 0, quotes: 0 };
}

export function errorDiag(item_index: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'error', item_index, code, message };
}

export type ParseOutcome =
  | { manifest: ImportManifest; diags: ImportDiagnostic[] }
  | { refusal: PreviewResponse };

export interface RefusalDiag {
  code: string;
  message: string;
}

export function refused(diag: RefusalDiag): ParseOutcome {
  return {
    refusal: {
      stats: emptyStats(),
      diagnostics: [errorDiag(-1, diag.code, diag.message)],
      error: diag.message
    }
  };
}

interface Parsed {
  manifest: ImportManifest;
  diagnostics: readonly ImportDiagnostic[];
}

function refusedBy(code: string, err: unknown): ParseOutcome {
  return refused({ code, message: (err as Error).message });
}

export async function parseLeg(parse: () => Parsed | Promise<Parsed>): Promise<ParseOutcome> {
  try {
    const parsed = await parse();
    return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
  } catch (err) {
    return refusedBy(CODE.parseFailed, err);
  }
}

export async function fetchAndParse<F>(
  fetch: () => Promise<F>,
  parse: (fetched: F) => Parsed | Promise<Parsed>
): Promise<ParseOutcome> {
  let fetched: F;
  try {
    fetched = await fetch();
  } catch (err) {
    return refusedBy(CODE.fetchFailed, err);
  }
  return parseLeg(() => parse(fetched));
}
