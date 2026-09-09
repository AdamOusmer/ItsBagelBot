// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// StreamElements: the channel JWT the user pastes is posted on the form and
// fetched with server-side (kappa v2), then translated by the shared parser.
import { fetchStreamElements, parseStreamElements } from '@bagel/kit/importer/streamelements';
import { CODE } from '@bagel/kit/importer/validate';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import {
  credentialShapeRefusal,
  missingAnyInput,
  type InputRefusal,
  type ServerSourceStrategy,
  type SourceInput
} from '../strategy';

async function streamelementsLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  let envelope: string;
  try {
    const fetched = await fetchStreamElements(req.credential ?? '');
    envelope = JSON.stringify({ commands: fetched.commands ?? [], timers: fetched.timers ?? [] });
  } catch (err) {
    return refused({ code: CODE.fetchFailed, message: (err as Error).message });
  }
  const parsed = parseStreamElements(envelope);
  return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
}

function acceptInput(input: SourceInput): InputRefusal {
  return missingAnyInput(input, 'Paste your StreamElements JWT first.') ?? credentialShapeRefusal(input.credential);
}

export const streamelementsSource: ServerSourceStrategy = {
  id: 'streamelements',
  acceptInput,
  leg: streamelementsLeg
};
