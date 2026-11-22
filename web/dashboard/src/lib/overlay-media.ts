// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { failureCode, MediaFailure, type MediaAdapter, type UploadedAsset } from '@bagel/kit/overlay-editor';
import { sendParts } from './overlay-parts';

export const ASSET_ENDPOINT = '/overlays/assets';
const NAME_LIMIT = 80;

const STATUS_CODES: Record<number, string> = {
  402: 'premium_required',
  413: 'asset_too_large',
  415: 'unsupported_media',
  429: 'rate_limited',
  507: 'quota_exceeded'
};

export function dashboardMedia(send: typeof fetch = fetch): MediaAdapter {
  return {
    async upload(file: File): Promise<UploadedAsset> {
      const response = await sendParts(ASSET_ENDPOINT, file, { name: file.name.slice(0, NAME_LIMIT) }, send);
      if (!response.ok) throw new MediaFailure(await failureCode(response, STATUS_CODES));
      return (await response.json()) as UploadedAsset;
    }
  };
}
