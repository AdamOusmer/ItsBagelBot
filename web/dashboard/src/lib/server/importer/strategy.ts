// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Cookies } from '@sveltejs/kit';
import type { ImportManifest, ImportSource } from '@bagel/kit';
import type { ImportPreviewRequest, ParseOutcome } from './engine';
import { streamelementsSource } from './sources/streamelements';
import { fossabotSource } from './sources/fossabot';
import { moobotSource } from './sources/moobot';
import { nightbotSource } from './sources/nightbot';
import { streamlabsDesktopSource } from './sources/streamlabs-desktop';
import { wizebotSource } from './sources/wizebot';

export interface SourceInput {
  credential: string;
  fileB64: string;
  preManifest?: ImportManifest;
}

export type InputRefusal = { status: number; error: string } | null;

export interface ServerSourceStrategy {
  id: ImportSource;
  acceptInput(input: SourceInput): InputRefusal;
  credential?(input: SourceInput, cookies: Cookies): string | null;
  connected?(cookies: Cookies): boolean;
  leg?(req: ImportPreviewRequest): Promise<ParseOutcome>;
  afterCommit?(cookies: Cookies): void;
}

export const SERVER_STRATEGIES: Record<ImportSource, ServerSourceStrategy> = {
  streamelements: streamelementsSource,
  fossabot: fossabotSource,
  moobot: moobotSource,
  nightbot: nightbotSource,
  streamlabs_desktop: streamlabsDesktopSource,
  wizebot: wizebotSource
};

export function missingAnyInput(input: SourceInput, error: string): InputRefusal {
  if (hasAnyInput(input)) return null;
  return { status: 400, error };
}

function hasAnyInput(input: SourceInput): boolean {
  if (input.preManifest !== undefined) return true;
  return [input.credential, input.fileB64].some(nonEmpty);
}

function nonEmpty(v: string): boolean {
  return v !== '';
}

export function fileSourceInput(input: SourceInput): InputRefusal {
  return missingAnyInput(input, 'Choose a file to upload.') ?? credentialShapeRefusal(input.credential);
}

const MAX_CREDENTIAL_LEN = 4096;
const JWT_SHAPE = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

export function credentialShapeRefusal(credential: string): InputRefusal {
  if (credential === '') return null;
  if (credential.length <= MAX_CREDENTIAL_LEN && JWT_SHAPE.test(credential)) return null;
  return {
    status: 400,
    error:
      'That does not look like a StreamElements JWT. Copy the whole token: three segments separated by dots, no spaces.'
  };
}
