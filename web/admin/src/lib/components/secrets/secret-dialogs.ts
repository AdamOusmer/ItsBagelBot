// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DbCredentialStatus } from '#lib/server/secrets';

export type SecretVerb = 'rotate' | 'set' | 'revoke';

type SecretDialog = {
  action: `?/${SecretVerb}`;
  title: string;
  body: string;
  label: string;
  cta: string;
  danger: boolean;
  needsUser: boolean;
  needsPassword: boolean;
  phrase: (service: DbCredentialStatus, dbUser: string) => string;
};

export const SECRET_DIALOGS = {
  rotate: {
    action: '?/rotate',
    title: 'admin.secrets.rotateTitle',
    body: 'admin.secrets.rotateBody',
    label: 'admin.secrets.rotate',
    cta: 'admin.secrets.rotate',
    danger: false,
    needsUser: false,
    needsPassword: false,
    phrase: (service) => `rotate ${service.id}`
  },
  set: {
    action: '?/set',
    title: 'admin.secrets.setTitle',
    body: 'admin.secrets.setBody',
    label: 'admin.secrets.set',
    cta: 'admin.secrets.setCta',
    danger: false,
    needsUser: true,
    needsPassword: true,
    phrase: (service) => `set ${service.id}`
  },
  revoke: {
    action: '?/revoke',
    title: 'admin.secrets.revokeTitle',
    body: 'admin.secrets.revokeBody',
    label: 'admin.secrets.revoke',
    cta: 'common.revoke',
    danger: true,
    needsUser: true,
    needsPassword: false,
    phrase: (_service, dbUser) => `revoke ${dbUser.trim()}`
  }
} as const satisfies Record<SecretVerb, SecretDialog>;
