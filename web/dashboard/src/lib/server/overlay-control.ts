// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import {
  assetDelete, assetList, feedControl, type FeedAction, type FeedControlArgs
} from '#lib/server/services/overlays';
import { ownerOrRefusal, refuse, refusal } from '#lib/server/overlay-upload';
import type { Session } from '#lib/server/session';

const DEMO = dev && env.DEMO === '1';

const NO_STORE = { 'cache-control': 'no-store' };
const ASSET_ID = /^[A-Za-z0-9_-]{1,96}$/;
const ACTIONS: ReadonlySet<string> = new Set<FeedAction>([
  'pause', 'resume', 'mute', 'unmute', 'skip', 'replay', 'clear', 'test',
  'subathon_add', 'subathon_pause', 'subathon_resume', 'subathon_reset'
]);

type ControlBody = { action?: unknown; args?: unknown };

export async function listAssets(session: Session | null | undefined): Promise<Response> {
  const owner = ownerOrRefusal(session);
  if (owner instanceof Response) return owner;
  try {
    const assets = DEMO ? (await import('#lib/server/demo-data')).demoAssets() : await assetList(owner);
    return json({ assets }, { headers: NO_STORE });
  } catch (err) {
    return refusal(err);
  }
}

export async function deleteAsset(session: Session | null | undefined, url: URL): Promise<Response> {
  const owner = ownerOrRefusal(session);
  if (owner instanceof Response) return owner;
  const id = url.searchParams.get('id') ?? '';
  if (!ASSET_ID.test(id)) return refuse('invalid', 400);
  try {
    if (DEMO) (await import('#lib/server/demo-data')).demoAssetDelete(id);
    else await assetDelete(owner, id);
    auditDashboardImpersonation(owner, 'overlays:asset_delete');
    return json({ deleted: true }, { headers: NO_STORE });
  } catch (err) {
    return refusal(err);
  }
}

function isAction(value: unknown): value is FeedAction {
  return typeof value === 'string' && ACTIONS.has(value);
}

function isArgs(value: unknown): value is FeedControlArgs {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function readControl(body: ControlBody | null): { action: FeedAction; args: FeedControlArgs } | null {
  if (!body || !isAction(body.action)) return null;
  const args = body.args ?? {};
  if (!isArgs(args)) return null;
  return { action: body.action, args };
}

export async function controlFeed(session: Session | null | undefined, request: Request): Promise<Response> {
  const owner = ownerOrRefusal(session);
  if (owner instanceof Response) return owner;
  const control = readControl((await request.json().catch(() => null)) as ControlBody | null);
  if (!control) return refuse('invalid', 400);
  try {
    const state = DEMO ? (await import('#lib/server/demo-data')).demoFeedControl(control) : await feedControl(owner, control);
    auditDashboardImpersonation(owner, `overlays:feed_${control.action}`);
    return json(state, { headers: NO_STORE });
  } catch (err) {
    return refusal(err);
  }
}
