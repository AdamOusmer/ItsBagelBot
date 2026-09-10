// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Session-bound OAuth state.
//
// A plain random state cookie proves only that the browser that started the
// flow is the browser that finished it. It does NOT prove the two were the
// same signed-in user, and the Discord install leg binds a guild to whoever
// the session says is logged in at the callback: an attacker who can get their
// own state/code pair into a victim's browser (a login-CSRF, or simply a
// shared machine where the session changed between the two legs) has the
// victim's account bind the attacker's server.
//
// So the cookie carries `state` plus an HMAC over `label:uid:state`. The uid is
// re-read from the session at the callback and folded back in, so a cookie
// minted under one account cannot validate under another. `label` separates
// the two Discord legs (user authorization vs bot install), which carry
// different consent and must never validate for each other.
//
// The key is the app's own SESSION_KEY (see server/session.ts): it is already
// per-app, already rotated as a unit with the session cookie, and rotating it
// invalidating in-flight OAuth states is correct rather than a bug.
import { createHmac, timingSafeEqual } from 'node:crypto';

/** `state` is oauth4webapi's base64url random; the separator can never occur
 *  inside it, so one split is unambiguous. */
const SEP = '.';

function mac(key: Buffer, label: string, uid: string, state: string): Buffer {
  const h = createHmac('sha256', key);
  // Length-prefixed rather than plain concatenation: without it a (label,uid)
  // pair could be re-split so that a different pair produced the same bytes.
  h.update(`${label.length}:${label}${uid.length}:${uid}${state.length}:${state}`);
  return h.digest();
}

/** The cookie value to store for `state`. */
export function sealOAuthState(key: Buffer, label: string, uid: string, state: string): string {
  return `${state}${SEP}${mac(key, label, uid, state).toString('base64url')}`;
}

/**
 * Reads a sealed cookie back, or `null` when it was not minted for this
 * (key, label, uid). Never throws: every failure shape a caller can be handed
 * -- absent, truncated, re-encoded, tampered -- is the same answer.
 */
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
