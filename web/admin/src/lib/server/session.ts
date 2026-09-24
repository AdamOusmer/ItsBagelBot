// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
import { createSessionCodec, decodeKey, SESSION_TTL_SECONDS } from '@bagel/kit/server/session';

export { SESSION_TTL_SECONDS };

export interface Session {
  user_id: string;
  login: string;
  display_name: string;
  role: 'streamer' | 'mod';
  iat: number;
  expires_at: number;
}

const codec = createSessionCodec<Session>(() => decodeKey(process.env.SESSION_KEY), 'admin-session');

export const seal = (s: Session): string => codec.seal(s);
export const open = (value: string): Session | null => codec.open(value, SESSION_TTL_SECONDS);

export const COOKIE = 'bagel_session';
