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

const HEX_RADIX = 16;
const HEX_WIDTH = 2;

export async function sha256Hex(file: Blob): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(HEX_RADIX).padStart(HEX_WIDTH, '0')).join('');
}

export function describeUpload(uploaded: UploadedAsset, sha256: string): OverlayAsset {
  return { type: uploaded.type, bytes: uploaded.bytes, w: uploaded.w, h: uploaded.h, duration_ms: uploaded.duration_ms, sha256 };
}

export const UPLOAD_ERRORS = [
  'unsupported_media', 'invalid_asset', 'asset_too_large', 'asset_dimensions', 'asset_duration', 'too_many_assets',
  'too_many_of_type', 'overlay_assets_too_large', 'premium_required', 'rate_limited', 'quota_exceeded', 'failed'
] as const;
export type UploadError = (typeof UPLOAD_ERRORS)[number];

export function uploadError(code: string): UploadError {
  return (UPLOAD_ERRORS as readonly string[]).includes(code) ? (code as UploadError) : 'failed';
}
