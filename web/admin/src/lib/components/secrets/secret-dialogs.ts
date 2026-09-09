// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DbCredentialStatus } from '$lib/server/secrets';

export type SecretVerb = 'rotate' | 'set' | 'revoke';

/**
 * The three secret mutations, as data.
 *
 * `action` is the server form-action name and is NOT free to rename: the audit
 * trail keys off the matching SecretSpec in +page.server.ts, and a rename here
 * would silently 404 the POST rather than fail the build.
 *
 * `phrase` mirrors the server's own type-to-confirm check exactly. It is
 * duplicated on purpose and the duplication is the point: the client shows the
 * operator the phrase, and the server -- which is the one that matters -- checks
 * it. If they ever disagree the form is refused, which is the safe direction.
 */
export type SecretDialog = {
  verb: SecretVerb;
  action: string;
  /** Catalog key for the dialog title. */
  title: string;
  /** Catalog key for the explanation above the fields. */
  body: string;
  /** Catalog key for the confirm button. */
  cta: string;
  danger: boolean;
  /** Which extra fields the dialog collects. */
  needsUser: boolean;
  needsPassword: boolean;
  phrase: (service: DbCredentialStatus, dbUser: string) => string;
};

export const SECRET_DIALOGS = {
  rotate: {
    verb: 'rotate',
    action: '?/rotate',
    title: 'admin.secrets.rotateTitle',
    body: 'admin.secrets.rotateBody',
    cta: 'admin.secrets.rotate',
    danger: false,
    needsUser: false,
    needsPassword: false,
    phrase: (service) => `rotate ${service.id}`
  },
  set: {
    verb: 'set',
    action: '?/set',
    title: 'admin.secrets.setTitle',
    body: 'admin.secrets.setBody',
    cta: 'admin.secrets.setCta',
    danger: false,
    needsUser: true,
    needsPassword: true,
    phrase: (service) => `set ${service.id}`
  },
  revoke: {
    verb: 'revoke',
    action: '?/revoke',
    title: 'admin.secrets.revokeTitle',
    body: 'admin.secrets.revokeBody',
    cta: 'admin.secrets.revoke',
    danger: true,
    needsUser: true,
    needsPassword: false,
    phrase: (_service, dbUser) => `revoke ${dbUser.trim()}`
  }
} as const satisfies Record<SecretVerb, SecretDialog>;
