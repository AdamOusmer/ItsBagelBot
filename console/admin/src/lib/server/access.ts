// Admin access control: a request must carry a valid session AND its user_id
// must appear in the ADMIN_USER_IDS allowlist (comma-separated). DEMO=1
// synthesizes an allowed session so the panel renders without auth wired up.
import { env } from '$env/dynamic/private';
import type { Session } from './session';

export const demoSession: Session = {
  user_id: 'demo-admin',
  login: 'itsmavey',
  display_name: 'Mavey',
  role: 'streamer',
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

export function isDemo(): boolean {
  return env.DEMO === '1';
}

function allowlist(): string[] {
  return (env.ADMIN_USER_IDS ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
}

// allowed reports whether the session belongs to a configured admin. In DEMO
// mode the synthesized demo id is always allowed.
export function allowed(session: Session | null): boolean {
  if (!session) return false;
  if (isDemo() && session.user_id === demoSession.user_id) return true;
  return allowlist().includes(session.user_id);
}
