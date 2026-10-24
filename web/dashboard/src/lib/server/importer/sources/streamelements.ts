// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchStreamElements, parseStreamElements } from '@bagel/kit/importer/streamelements';
import { fetchAndParse, type ImportPreviewRequest, type ParseOutcome } from '../outcome';
import {
  credentialShapeRefusal,
  missingAnyInput,
  type InputRefusal,
  type ServerSourceStrategy,
  type SourceInput
} from '../strategy';

function streamelementsLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  return fetchAndParse(
    async () => {
      const fetched = await fetchStreamElements(req.credential ?? '');
      return JSON.stringify({ commands: fetched.commands ?? [], timers: fetched.timers ?? [] });
    },
    parseStreamElements
  );
}

function acceptInput(input: SourceInput): InputRefusal {
  return missingAnyInput(input, 'Paste your StreamElements JWT first.') ?? credentialShapeRefusal(input.credential);
}

export const streamelementsSource: ServerSourceStrategy = {
  id: 'streamelements',
  acceptInput,
  leg: streamelementsLeg
};
