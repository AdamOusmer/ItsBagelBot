// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchWizebot, parseWizebot } from '@bagel/kit/importer/wizebot';
import { fetchAndParse, type ImportPreviewRequest, type ParseOutcome } from '../outcome';
import { missingAnyInput, type InputRefusal, type ServerSourceStrategy, type SourceInput } from '../strategy';

const HANDLE_SHAPE = /^[A-Za-z0-9][A-Za-z0-9-]{0,24}$/;

function wizebotLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  return fetchAndParse(() => fetchWizebot(req.credential ?? ''), parseWizebot);
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
