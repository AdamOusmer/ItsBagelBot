// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchFossabot, parseFossabot } from '@bagel/kit/importer/fossabot';
import { FOSSABOT_HANDLE_SHAPE, MAX_HANDLE_LEN } from '@bagel/kit/importer/strategy';
import { CODE } from '@bagel/kit/importer/validate';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import type { InputRefusal, ServerSourceStrategy, SourceInput } from '../strategy';

async function fossabotLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  let envelope: Uint8Array;
  try {
    const fetched = await fetchFossabot(req.credential ?? '');
    envelope = new TextEncoder().encode(JSON.stringify(fetched));
  } catch (err) {
    return refused({ code: CODE.fetchFailed, message: (err as Error).message });
  }
  try {
    const parsed = parseFossabot(envelope);
    return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
  } catch (err) {
    return refused({ code: CODE.parseFailed, message: (err as Error).message });
  }
}

function acceptInput(input: SourceInput): InputRefusal {
  const handle = input.credential.trim();
  if (handle === '') return { status: 400, error: 'Enter your Fossabot channel name first.' };
  if (handle.length <= MAX_HANDLE_LEN && FOSSABOT_HANDLE_SHAPE.test(handle)) return null;
  return { status: 400, error: 'That does not look like a Fossabot channel name.' };
}

export const fossabotSource: ServerSourceStrategy = {
  id: 'fossabot',
  acceptInput,
  leg: fossabotLeg
};
