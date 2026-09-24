// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { parseStreamLabsDesktop } from '@bagel/kit/importer/streamlabs-desktop';
import { CODE } from '@bagel/kit/importer/validate';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import { fileSourceInput, type ServerSourceStrategy } from '../strategy';

const MAX_DECODED_FILE_BYTES = 25 << 20;

async function streamlabsDesktopLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  if (!req.file_b64) {
    return refused({ code: CODE.fileRequired, message: 'upload your Chatbot.db file' });
  }

  const file = Buffer.from(req.file_b64, 'base64');
  if (file.byteLength > MAX_DECODED_FILE_BYTES) {
    return refused({
      code: CODE.fileTooLarge,
      message: `file is ${file.byteLength} bytes; the limit is ${MAX_DECODED_FILE_BYTES}`
    });
  }

  try {
    const parsed = await parseStreamLabsDesktop(file);
    return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
  } catch (err) {
    return refused({ code: CODE.parseFailed, message: (err as Error).message });
  }
}

export const streamlabsDesktopSource: ServerSourceStrategy = {
  id: 'streamlabs_desktop',
  acceptInput: fileSourceInput,
  leg: streamlabsDesktopLeg
};
