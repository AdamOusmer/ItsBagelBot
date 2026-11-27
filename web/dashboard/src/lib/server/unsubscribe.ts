// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { RpcError } from '@bagel/kit/server/nats';
import { marketingLink, type MarketingLink } from '#lib/server/services';

const ONE_CLICK_FIELD = 'List-Unsubscribe';
const ONE_CLICK_VALUE = 'One-Click';
const MAX_BODY_BYTES = 1024;
const GONE_CODES = new Set(['invalid', 'not_found']);

export type LinkView =
  | { state: 'ready'; status: MarketingLink['status']; maskedEmail: string }
  | { state: 'invalid' }
  | { state: 'unavailable' };

export function linkFailure(err: unknown): 'invalid' | 'unavailable' {
  return err instanceof RpcError && GONE_CODES.has(err.code) ? 'invalid' : 'unavailable';
}

export async function readLink(token: string): Promise<LinkView> {
  try {
    const link = await marketingLink(token);
    return { state: 'ready', status: link.status, maskedEmail: link.maskedEmail };
  } catch (err) {
    return { state: linkFailure(err) };
  }
}

export function isOneClick(form: { get(name: string): unknown }): boolean {
  return form.get(ONE_CLICK_FIELD) === ONE_CLICK_VALUE;
}

export async function oneClickRequest(request: Request): Promise<boolean> {
  const type = (request.headers.get('content-type') ?? '').split(';', 1)[0].trim().toLowerCase();
  if (type !== 'application/x-www-form-urlencoded') return false;
  const body = await request.text();
  if (Buffer.byteLength(body) > MAX_BODY_BYTES) return false;
  return isOneClick(new URLSearchParams(body));
}
