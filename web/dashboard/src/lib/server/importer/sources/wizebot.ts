// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Wizebot: the channel name the user types is posted on the form as the
// credential field (it is a public handle, not a secret: the same string sits
// in the URL of the channel's own streaming website), the leg replays that
// site's session to read its published command list, and the shared parser
// translates it.
//
// The leg runs here rather than in the browser because reading the list means
// carrying a cross-origin session cookie, which no page bundle may do.
import { fetchWizebot, parseWizebot } from '@bagel/kit/importer/wizebot';
import { CODE } from '@bagel/kit/importer/validate';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import { missingAnyInput, type InputRefusal, type ServerSourceStrategy, type SourceInput } from '../strategy';

// HANDLE_SHAPE mirrors the client strategy's gate and the fetch layer's own,
// restated so a hand-made post is refused with the same
// prose the page shows before anything reaches the transport.
const HANDLE_SHAPE = /^[A-Za-z0-9][A-Za-z0-9-]{0,24}$/;

async function wizebotLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  let list: Uint8Array;
  try {
    list = await fetchWizebot(req.credential ?? '');
  } catch (err) {
    return refused({ code: CODE.fetchFailed, message: (err as Error).message });
  }
  try {
    const parsed = parseWizebot(list);
    return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
  } catch (err) {
    return refused({ code: CODE.parseFailed, message: (err as Error).message });
  }
}

function acceptInput(input: SourceInput): InputRefusal {
  return (
    missingAnyInput(input, 'Enter your Wizebot channel name first.') ?? handleShapeRefusal(input.credential)
  );
}

function handleShapeRefusal(handle: string): InputRefusal {
  if (HANDLE_SHAPE.test(handle.trim())) return null;
  return { status: 400, error: 'That does not look like a Wizebot channel name.' };
}

export const wizebotSource: ServerSourceStrategy = {
  id: 'wizebot',
  acceptInput,
  leg: wizebotLeg
};
