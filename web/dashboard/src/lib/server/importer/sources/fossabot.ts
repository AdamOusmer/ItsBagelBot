// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchFossabot, parseFossabot } from '@bagel/kit/importer/fossabot';
import { FOSSABOT_HANDLE_SHAPE, MAX_HANDLE_LEN } from '@bagel/kit/importer/strategy';
import { fetchAndParse, type ImportPreviewRequest, type ParseOutcome } from '../outcome';
import type { InputRefusal, ServerSourceStrategy, SourceInput } from '../strategy';

function fossabotLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  return fetchAndParse(
    async () => new TextEncoder().encode(JSON.stringify(await fetchFossabot(req.credential ?? ''))),
    parseFossabot
  );
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
