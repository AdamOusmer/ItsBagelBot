// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Fossabot: the channel name the user types is posted on the form and read
// server-side against Fossabot's public cached API, then translated by the
// shared parser. There is no credential and no connect step: the feed this
// reaches is the same commands directory any viewer can open at
// fossabot.com/<login>/commands, which is also why nothing about it is
// cached or stored here beyond the preview the user is about to review.
import { fetchFossabot, parseFossabot } from '@bagel/shared/importer/fossabot';
import { FOSSABOT_HANDLE_SHAPE, MAX_HANDLE_LEN } from '@bagel/shared/importer/strategy';
import { CODE } from '@bagel/shared/importer/validate';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import type { InputRefusal, ServerSourceStrategy, SourceInput } from '../strategy';

// fossabotLeg resolves the channel, reads its directory and parses the stapled
// envelope. The fetch and the parse are kept apart so the refusal names which
// half failed: "no Fossabot channel named …" is a typo the user fixes, a parse
// failure is a shape change on their side.
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

// acceptInput refuses an empty or malformed handle before any transport, with
// the same gate the page runs client-side (@bagel/shared/importer/strategy owns
// the regex). A handle is not a secret, so it is safe to say what was wrong.
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
