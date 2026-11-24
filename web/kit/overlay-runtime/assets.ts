// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { OVERLAY_CDN_PREFIX } from '../lib/overlay-cdn';
import type { AssetType, OverlayAsset } from '../lib/overlay-document';

const PREFIX = 'asset:';

export type ResolvedAsset = { url: string; type: AssetType };

export class Assets {
  constructor(private readonly entries: Record<string, OverlayAsset> = {}) {}

  resolve(ref: string): ResolvedAsset | undefined {
    if (!ref.startsWith(PREFIX)) return;
    const id = ref.slice(PREFIX.length);
    const asset = Object.hasOwn(this.entries, id) ? this.entries[id] : undefined;
    if (!asset?.url?.startsWith(OVERLAY_CDN_PREFIX)) return;
    return { url: asset.url, type: asset.type };
  }
}
