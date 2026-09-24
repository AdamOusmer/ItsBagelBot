// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
import {
  createSessionCodec,
  decodeKey,
  IMPERSONATION_TTL_SECONDS,
  SESSION_TTL_SECONDS
} from '@bagel/kit/server/session';

export { IMPERSONATION_TTL_SECONDS, SESSION_TTL_SECONDS };

export interface Session {
  user_id: string;
  login: string;
  display_name: string;
  role: 'streamer' | 'mod';
  sid: string;
  iat: number;
  expires_at: number;
  impersonator_id?: string;
  impersonator_login?: string;
  delegate_of?: string;
  delegate_login?: string;
  sections?: string[];
}

const codec = createSessionCodec<Session>(() => decodeKey(process.env.SESSION_KEY), 'dashboard-session');

export const seal = (s: Session): string => codec.seal(s);

export const open = (value: string): Session | null => {
  const s = codec.open(value);
  if (!s) return null;
  const cap = s.impersonator_id ? IMPERSONATION_TTL_SECONDS : SESSION_TTL_SECONDS;
  if (Date.now() / 1000 - s.iat > cap) return null;
  return s;
};

export const COOKIE = 'bagel_session';
export const ACCOUNT_DELETED_COOKIE = 'bagel_account_deleted';

export const CURSOR_COOKIE = 'bagel_cursor';
