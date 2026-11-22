// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { failureCode, importError, ImportFailure, type ImportResult } from '@bagel/kit/overlay-editor';

export const IMPORT_ENDPOINT = '/overlays/imports';

export async function importArchive(file: File, dryRun: boolean, send: typeof fetch = fetch): Promise<ImportResult> {
  const query = dryRun ? '?dry_run=1' : '';
  const response = await send(`${IMPORT_ENDPOINT}${query}`, {
    method: 'POST',
    headers: { 'content-type': 'application/zip' },
    body: file
  });
  if (!response.ok) throw new ImportFailure(importError(await failureCode(response, { 402: 'premium_required' })));
  return (await response.json()) as ImportResult;
}
