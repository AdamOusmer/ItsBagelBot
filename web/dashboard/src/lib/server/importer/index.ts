// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Barrel for the config importer. `$lib/server/importer` resolved to a single
// file until the strategy split (2026-09-07); it keeps resolving to the same
// two functions so no caller moved.
export { commitImport, previewImport } from './engine';
export type { ImportCommitRequest, ImportPreviewRequest } from './engine';
export { SERVER_STRATEGIES } from './strategy';
export type { InputRefusal, ServerSourceStrategy, SourceInput } from './strategy';
