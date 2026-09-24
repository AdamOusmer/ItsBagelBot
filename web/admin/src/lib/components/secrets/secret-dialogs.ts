// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DbCredentialStatus } from '$lib/server/secrets';

export type SecretVerb = 'rotate' | 'set' | 'revoke';

/** `action` is the server form-action name: a rename silently 404s the POST. */
export type SecretDialog = {
  verb: SecretVerb;
  action: string;
  title: string;
  body: string;
  cta: string;
  danger: boolean;
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
