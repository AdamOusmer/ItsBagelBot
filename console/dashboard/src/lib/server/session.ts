// Dashboard session: the shared AES-256-GCM codec instantiated over the
// dashboard's own Session shape and its own isolated SESSION_KEY.
//
// SESSION_KEY is read from process.env, NOT $env/dynamic/private: this module is
// in the boot import graph (hooks.server.ts -> session), and even importing the
// dynamic-env proxy there deadlocks server.init (exit 13). process.env carries
// the same runtime value; the key getter runs per seal/open (request time).
import {
  createSessionCodec,
  decodeKey,
  IMPERSONATION_TTL_SECONDS,
  SESSION_TTL_SECONDS
} from '@bagel/shared/server/session';

export { IMPERSONATION_TTL_SECONDS, SESSION_TTL_SECONDS };

export interface Session {
  user_id: string;
  login: string;
  display_name: string;
  role: 'streamer' | 'mod';
  iat: number;
  expires_at: number;
  // Set only when an admin is viewing this dashboard "as" the user. Carries the
  // acting admin so every write during the session is audited back to them.
  impersonator_id?: string;
  impersonator_login?: string;
  // Set only when this is a delegated session: the invitee logs in with their
  // own Twitch account but operates the owner's dashboard, limited to the
  // granted sections. delegate_of carries the owner's user_id.
  delegate_of?: string;
  delegate_login?: string;
  sections?: string[];
}

const codec = createSessionCodec<Session>(() => decodeKey(process.env.SESSION_KEY));

export const seal = (s: Session): string => codec.seal(s);

// open applies the lifetime cap for the session's kind, measured from iat.
// An impersonation ("view as") session is hard-capped at 1h no matter what
// expires_at a mint or re-seal path wrote; everything else caps at the normal
// login TTL.
export const open = (value: string): Session | null => {
  const s = codec.open(value);
  if (!s) return null;
  const cap = s.impersonator_id ? IMPERSONATION_TTL_SECONDS : SESSION_TTL_SECONDS;
  if (Date.now() / 1000 - s.iat > cap) return null;
  return s;
};

export const COOKIE = 'bagel_session';
export const ACCOUNT_DELETED_COOKIE = 'bagel_account_deleted';
