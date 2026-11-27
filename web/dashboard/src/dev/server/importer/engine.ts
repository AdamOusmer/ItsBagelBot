// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CommitResponse, ImportSource, PreviewResponse } from '@bagel/kit';
import type { ImportCommitRequest, ImportPreviewRequest } from '#lib/server/importer/engine';
import { demoImportCommit, demoImportPreview } from '../../import-fixtures';

export * from '#lib/server/importer/engine';

export async function previewImport(_s: unknown, req: ImportPreviewRequest): Promise<PreviewResponse> {
  return demoImportPreview(req.source as ImportSource);
}

export async function commitImport(_s: unknown, req: ImportCommitRequest): Promise<CommitResponse> {
  return demoImportCommit(req.manifest, req.overwrite);
}
