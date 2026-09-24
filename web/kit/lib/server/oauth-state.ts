// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHmac, timingSafeEqual } from 'node:crypto';

const SEP = '.';

function mac(key: Buffer, label: string, uid: string, state: string): Buffer {
  const h = createHmac('sha256', key);
  // Length-prefixed so a different (label, uid) split cannot produce the same bytes.
  h.update(`${label.length}:${label}${uid.length}:${uid}${state.length}:${state}`);
  return h.digest();
}

export function sealOAuthState(key: Buffer, label: string, uid: string, state: string): string {
  return `${state}${SEP}${mac(key, label, uid, state).toString('base64url')}`;
}

export function openOAuthState(key: Buffer, label: string, uid: string, cookie: string): string | null {
  const cut = cookie.lastIndexOf(SEP);
  if (cut <= 0) return null;
  const state = cookie.slice(0, cut);
  const got = Buffer.from(cookie.slice(cut + 1), 'base64url');
  const want = mac(key, label, uid, state);
  if (got.length !== want.length) return null;
  if (!timingSafeEqual(got, want)) return null;
  return state;
}
