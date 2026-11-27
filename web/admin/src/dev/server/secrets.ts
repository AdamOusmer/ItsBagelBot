// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { serviceIds, type DbCredentialInput, type DbCredentialStatus, type ScopeReport, type SecretServiceId } from '#lib/server/secrets';
import { demoSecretsBundle } from '../fixtures';

export * from '#lib/server/secrets';

const rotated = (id: SecretServiceId) => ({ dbUser: `${id.replace(/-/g, '_')}_svc_r${Date.now().toString(36)}` });

export async function credentialStatuses(): Promise<DbCredentialStatus[]> {
  return demoSecretsBundle(serviceIds()).services;
}

export async function scopeReport(): Promise<ScopeReport> {
  return demoSecretsBundle(serviceIds()).scope;
}

export async function rotateCredential(id: SecretServiceId): Promise<{ dbUser: string }> {
  return rotated(id);
}

export async function setCredential(_id: SecretServiceId, input: DbCredentialInput): Promise<{ dbUser: string }> {
  return { dbUser: input.dbUser };
}

export async function revokeCredential(_id: SecretServiceId, input: Pick<DbCredentialInput, 'dbUser'>): Promise<{ dbUser: string }> {
  return { dbUser: input.dbUser };
}
