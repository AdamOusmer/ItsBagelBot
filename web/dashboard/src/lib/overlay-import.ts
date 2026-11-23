// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { failureCode, importError, ImportFailure, type ImportResult } from '@bagel/kit/overlay-editor';
import { sendParts } from './overlay-parts';

export const IMPORT_ENDPOINT = '/overlays/imports';

export async function importArchive(file: File, dryRun: boolean, send: typeof fetch = fetch): Promise<ImportResult> {
  const response = await sendParts(IMPORT_ENDPOINT, file, { dry_run: dryRun }, send);
  if (!response.ok) throw new ImportFailure(importError(await failureCode(response, { 403: 'alpha_required' })));
  return (await response.json()) as ImportResult;
}
