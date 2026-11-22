// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { env } from '@bagel/kit/server/env';
import { RpcError } from '@bagel/kit/server/nats';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { assetBegin, importBegin, type UploadTicket } from '#lib/server/services/overlays';
import type { Session } from '#lib/server/session';

export type UploadKind = 'assets' | 'imports';

type BeginBody = { declared_size?: unknown; name?: unknown; dry_run?: unknown };

const BEGIN: Record<UploadKind, (session: Session, body: BeginBody) => Promise<UploadTicket>> = {
  assets: (session, body) => assetBegin(session, { declared_size: Number(body.declared_size), display_name: String(body.name ?? '') }),
  imports: (session, body) => importBegin(session, { declared_size: Number(body.declared_size), dry_run: body.dry_run === true })
};

const REFUSAL_STATUS: Record<string, number> = { invalid: 400, forbidden: 403, not_found: 404, conflict: 409 };
const TICKET = /^[A-Za-z0-9_.-]{16,1024}$/;
const PART = /^[1-9][0-9]{0,3}$/;

const refuse = (error: string, status: number) => json({ error }, { status, headers: { 'cache-control': 'no-store' } });

function refusal(err: unknown): Response {
  if (!(err instanceof RpcError)) return refuse('unavailable', 503);
  if (err.message === 'rate_limited') return refuse(err.message, 429);
  return refuse(err.message, REFUSAL_STATUS[err.code] ?? 503);
}

function ownerOrRefusal(session: Session | null | undefined): Session | Response {
  if (!session) return refuse('not_signed_in', 401);
  return hasOwnerAccess(session) ? session : refuse('owner_only', 403);
}

export async function beginUpload(kind: UploadKind, session: Session | null | undefined, request: Request): Promise<Response> {
  const owner = ownerOrRefusal(session);
  if (owner instanceof Response) return owner;
  const body = (await request.json().catch(() => null)) as BeginBody | null;
  if (!body) return refuse('invalid', 400);
  try {
    const ticket = await BEGIN[kind](owner, body);
    auditDashboardImpersonation(owner, `overlays:${kind}_upload`);
    return json(ticket, { headers: { 'cache-control': 'no-store' } });
  } catch (err) {
    return refusal(err);
  }
}

function partQuery(url: URL): string | null {
  const part = url.searchParams.get('part');
  if (part !== null) return PART.test(part) ? `part=${part}` : null;
  if (url.searchParams.get('done') !== '1') return null;
  return url.searchParams.get('dry_run') === '1' ? 'done=1&dry_run=1' : 'done=1';
}

function partInit(request: Request, size: number): RequestInit {
  const ca = env.OVERLAYS_INTERNAL_CA_PEM;
  return {
    method: 'PUT',
    body: size ? request.body : null,
    headers: { 'content-length': String(size), 'content-type': 'application/octet-stream' },
    duplex: 'half',
    ...(ca ? { tls: { ca } } : {})
  } as RequestInit;
}

type PartRequest = { ticket: string; query: string; size: number };

function lengthOf(request: Request): number {
  const size = Number(request.headers.get('content-length') ?? 0);
  return Number.isSafeInteger(size) && size >= 0 ? size : -1;
}

function parsePart(url: URL, request: Request): PartRequest | Response {
  const ticket = url.searchParams.get('ticket') ?? '';
  const query = partQuery(url);
  const size = lengthOf(request);
  if (!query || size < 0) return refuse('invalid', 400);
  if (!TICKET.test(ticket)) return refuse('invalid', 400);
  if (query.startsWith('part=') && size === 0) return refuse('length_required', 411);
  return { ticket, query, size };
}

export async function relayPart(kind: UploadKind, session: Session | null | undefined, request: Request, url: URL): Promise<Response> {
  const owner = ownerOrRefusal(session);
  if (owner instanceof Response) return owner;
  const part = parsePart(url, request);
  if (part instanceof Response) return part;
  try {
    const upstream = await fetch(`${env.OVERLAYS_INTERNAL_URL}/${kind}/${part.ticket}?${part.query}`, partInit(request, part.size));
    return new Response(upstream.body, {
      status: upstream.status,
      headers: { 'content-type': 'application/json', 'cache-control': 'no-store' }
    });
  } catch {
    return refuse('unavailable', 503);
  }
}
