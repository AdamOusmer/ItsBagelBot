// Admin session: the shared AES-256-GCM codec instantiated over the admin's own
// Session shape and its OWN isolated SESSION_KEY (separate Doppler config).
// Secrets are never shared with the dashboard, so an admin session can only be
// minted by the admin's own OAuth callback.
//
// SESSION_KEY is read from process.env, NOT $env/dynamic/private: this module is
// in the boot import graph (hooks.server.ts -> session), and even importing the
// dynamic-env proxy there deadlocks server.init (exit 13). process.env carries
// the same runtime value; the key getter runs per seal/open (request time).
import { createSessionCodec, decodeKey } from '@bagel/shared/server/session';

export interface Session {
  user_id: string;
  login: string;
  display_name: string;
  role: 'streamer' | 'mod';
  expires_at: number;
}

const codec = createSessionCodec<Session>(() => decodeKey(process.env.SESSION_KEY));

export const seal = (s: Session): string => codec.seal(s);
export const open = (value: string): Session | null => codec.open(value);

export const COOKIE = 'bagel_session';
