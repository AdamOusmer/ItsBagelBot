// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { deserialize } from '$app/forms';
import type { OverlayDocument } from '@bagel/kit/overlay-document';

export type SaveConflict = { id: number; revision: number; document: unknown };
export type SaveReply =
  | { ok: true; revision: number; id: number; url?: string }
  | { ok: false; error: string; current?: SaveConflict };

export async function postSave(id: number, doc: OverlayDocument, send: typeof fetch = fetch): Promise<SaveReply> {
  const body = new FormData();
  body.set('id', String(id));
  body.set('document', JSON.stringify(doc));
  const response = await send('?/save', { method: 'POST', body, headers: { 'x-sveltekit-action': 'true' } });
  const result = deserialize(await response.text());
  if (result.type === 'success') {
    const url = result.data?.url;
    return { ok: true, revision: Number(result.data?.revision), id: Number(result.data?.id), url: typeof url === 'string' ? url : undefined };
  }
  const failure = result.type === 'failure' ? result.data : undefined;
  const message = failure?.error;
  return { ok: false, error: typeof message === 'string' ? message : '', current: failure?.current as SaveConflict | undefined };
}
