// Dashboard session: the shared AES-256-GCM codec instantiated over the
// dashboard's own Session shape and its own isolated SESSION_KEY.
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
export const open = (value: string): Session | null => codec.open(value);

export const COOKIE = 'bagel_session';
export const ACCOUNT_DELETED_COOKIE = 'bagel_account_deleted';
