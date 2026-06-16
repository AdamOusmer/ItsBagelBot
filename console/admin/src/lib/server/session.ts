// Encrypted session cookie: AES-256-GCM, layout base64url(nonce[12] || ct||tag),
// AAD "session". Key from SESSION_KEY (base64, 32 bytes). The wire format matches
// the dashboard tier, but the admin uses its OWN isolated SESSION_KEY (separate
// Doppler config); secrets are never shared, so an admin session can only be
// minted by the admin's own OAuth callback.
import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';
import { env } from '$env/dynamic/private';

export interface Session {
  user_id: string;
  login: string;
  display_name: string;
  role: 'streamer' | 'mod';
  expires_at: number;
}

const AAD = Buffer.from('session');

function key(): Buffer {
  const b64 = env.SESSION_KEY;
  if (!b64) throw new Error('SESSION_KEY not set');
  const k = Buffer.from(b64, 'base64');
  if (k.length !== 32) throw new Error('SESSION_KEY must decode to 32 bytes');
  return k;
}

export function seal(s: Session): string {
  const iv = randomBytes(12);
  const c = createCipheriv('aes-256-gcm', key(), iv);
  c.setAAD(AAD);
  const ct = Buffer.concat([c.update(JSON.stringify(s), 'utf8'), c.final()]);
  return Buffer.concat([iv, ct, c.getAuthTag()]).toString('base64url');
}

export function open(value: string): Session | null {
  try {
    const raw = Buffer.from(value, 'base64url');
    if (raw.length < 12 + 16) return null;
    const iv = raw.subarray(0, 12);
    const tag = raw.subarray(raw.length - 16);
    const ct = raw.subarray(12, raw.length - 16);
    const d = createDecipheriv('aes-256-gcm', key(), iv);
    d.setAAD(AAD);
    d.setAuthTag(tag);
    const pt = Buffer.concat([d.update(ct), d.final()]).toString('utf8');
    const s = JSON.parse(pt) as Session;
    if (Date.now() / 1000 > s.expires_at) return null;
    return s;
  } catch {
    return null;
  }
}

export const COOKIE = 'bagel_session';
