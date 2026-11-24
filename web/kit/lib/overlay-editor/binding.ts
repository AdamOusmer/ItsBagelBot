// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayAsset } from '../overlay-document';
import type { MediaSlot } from './fields';

export type SaveOutcome = { ok: true; revision: number } | { ok: false; error: string };

export type MediaBinding = {
  upload: (file: File, slot: MediaSlot) => Promise<string>;
  assets: Record<string, OverlayAsset>;
};
