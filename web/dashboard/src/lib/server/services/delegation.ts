// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { defineRead, READ_TIMEOUT_MS } from '@bagel/kit/server/service';
import { SUB } from './subjects';
import { fabric, invalidate } from './fabric';

export type DelegationGrant = {
  token: string;
  sections: string[];
  delegate_login: string;
  consumed: boolean;
};

export type GrantRef = { ownerId: string; token: string };

export async function delegationCreate(args: {
  owner: { id: string; login: string };
  sections: string[];
}): Promise<string> {
  const r = await rpc<{ token?: string; error?: string }>(`${SUB.delegation}.create`, {
    owner_user_id: args.owner.id,
    owner_login: args.owner.login,
    sections: args.sections
  });
  if (!r.token) throw new Error(r.error ?? 'create failed');
  invalidate(`delegations:${args.owner.id}`);
  return r.token;
}

export const delegationGet = defineRead({
  subject: `${SUB.delegation}.get`,
  request: (token: string) => ({ token }),
  map: (r: {
    owner_user_id?: string;
    owner_login?: string;
    sections?: string[];
    consumed?: boolean;
    error?: string;
  }): {
    owner_user_id: string;
    owner_login: string;
    sections: string[];
    consumed: boolean;
  } | null => {
    if (r.error || !r.owner_user_id) return null;
    return {
      owner_user_id: r.owner_user_id,
      owner_login: r.owner_login ?? '',
      sections: r.sections ?? [],
      consumed: r.consumed === true
    };
  },
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (token: string) => `delegation-token:${token}`,
    policy: POLICY.entity
  }
});

export async function delegationConsume(args: {
  token: string;
  delegate: { id: string; login: string };
}): Promise<{ ok: boolean; owner_user_id?: string; owner_login?: string; sections?: string[]; error?: string }> {
  const r = await rpc<{ ok: boolean; owner_user_id?: string; owner_login?: string; sections?: string[]; error?: string }>(`${SUB.delegation}.consume`, {
    token: args.token,
    delegate_user_id: args.delegate.id,
    delegate_login: args.delegate.login
  });
  invalidate(`delegation-token:${args.token}`, `delegations:${args.delegate.id}`);
  if (r.owner_user_id) invalidate(`delegations:${r.owner_user_id}`);
  return r;
}

export const delegationList = defineRead({
  subject: `${SUB.delegation}.list`,
  request: (ownerId: string) => ({ owner_user_id: ownerId }),
  map: (r: { grants?: DelegationGrant[] }) => r.grants ?? [],
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (ownerId: string) => `delegations:${ownerId}:given`,
    policy: POLICY.entity
  }
});

function invalidateGrant(grant: GrantRef, delegateId?: string): void {
  invalidate(
    `delegation-token:${grant.token}`,
    `delegations:${grant.ownerId}`,
    ...(delegateId ? [`delegations:${delegateId}`] : [])
  );
}

type GrantChangeReply = { ok?: boolean; error?: string; delegate_user_id?: string };

export async function delegationUpdate(args: GrantRef & { sections: string[] }): Promise<void> {
  const r = await rpc<GrantChangeReply>(`${SUB.delegation}.update`, {
    owner_user_id: args.ownerId,
    token: args.token,
    sections: args.sections
  });
  if (!r.ok) throw new Error(r.error ?? 'update failed');
  invalidateGrant(args, r.delegate_user_id);
}

export async function delegationRevoke(grant: GrantRef): Promise<void> {
  const r = await rpc<GrantChangeReply>(`${SUB.delegation}.revoke`, {
    owner_user_id: grant.ownerId,
    token: grant.token
  });
  if (!r.ok) throw new Error(r.error ?? 'revoke failed');
  invalidateGrant(grant, r.delegate_user_id);
}

export async function delegationOptOut(args: { delegateId: string; ownerId: string }): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.delegation}.opt_out`, {
    delegate_user_id: args.delegateId,
    owner_user_id: args.ownerId
  });
  if (!r.ok) throw new Error(r.error ?? 'opt out failed');
  invalidate(`delegations:${args.delegateId}`, `delegations:${args.ownerId}`);
}

export const delegationAccess = defineRead({
  subject: `${SUB.delegation}.access`,
  request: (delegateId: string) => ({ delegate_user_id: delegateId }),
  map: (r: {
    grants?: { owner_user_id: string; owner_login: string; sections: string[] }[];
  }) => r.grants ?? [],
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (delegateId: string) => `delegations:${delegateId}:access`,
    policy: POLICY.entity
  }
});
