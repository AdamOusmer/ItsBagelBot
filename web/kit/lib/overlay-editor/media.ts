// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { assetLimits, type AssetType, type OverlayAsset } from '../overlay-document';
import type { MediaSlot } from './fields';

export type UploadedAsset = {
  asset_id: string;
  url: string;
  type: AssetType;
  w: number;
  h: number;
  duration_ms: number;
  bytes: number;
  sha256: string;
  display_name?: string;
};

export interface MediaAdapter {
  upload(file: File): Promise<UploadedAsset>;
}

export class MediaFailure extends Error {
  constructor(readonly code: string) {
    super(code);
  }
}

export const slotTypes: Record<MediaSlot, readonly AssetType[]> = {
  image: ['png', 'webp', 'jpeg', 'gif'],
  visual: ['png', 'webp', 'jpeg', 'gif', 'webm'],
  video: ['webm'],
  sound: ['mp3', 'ogg', 'wav']
};

const MIME: Record<AssetType, string> = {
  png: 'image/png', webp: 'image/webp', jpeg: 'image/jpeg', gif: 'image/gif', webm: 'video/webm',
  mp3: 'audio/mpeg', ogg: 'audio/ogg', wav: 'audio/wav', woff2: 'font/woff2', widget: 'application/json'
};

export const slotAccept = (slot: MediaSlot): string => slotTypes[slot].map((type) => MIME[type]).join(',');

export const slotMaxBytes = (slot: MediaSlot): number => Math.max(...slotTypes[slot].map((type) => assetLimits[type].maxBytes));

export const acceptsType = (slot: MediaSlot, type: AssetType): boolean => slotTypes[slot].includes(type);

export function describeUpload(uploaded: UploadedAsset): OverlayAsset {
  const { type, bytes, w, h, duration_ms, sha256, url } = uploaded;
  return { type, bytes, w, h, duration_ms, sha256, url };
}

export const UPLOAD_ERRORS = [
  'unsupported_media', 'invalid_asset', 'asset_too_large', 'asset_dimensions', 'asset_duration', 'too_many_assets',
  'too_many_of_type', 'overlay_assets_too_large', 'alpha_required', 'rate_limited', 'quota_exceeded', 'failed'
] as const;
export type UploadError = (typeof UPLOAD_ERRORS)[number];

const SERVER_CODES: Record<string, UploadError> = {
  too_large: 'asset_too_large',
  unsupported_svg: 'unsupported_media',
  unsupported_mp4: 'unsupported_media',
  unsupported_html: 'unsupported_media',
  unsupported_codec: 'unsupported_media',
  asset_frames: 'asset_dimensions',
  size_mismatch: 'invalid_asset'
};

export function uploadError(code: string): UploadError {
  if (Object.hasOwn(SERVER_CODES, code)) return SERVER_CODES[code]!;
  return (UPLOAD_ERRORS as readonly string[]).includes(code) ? (code as UploadError) : 'failed';
}
