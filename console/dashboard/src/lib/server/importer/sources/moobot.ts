// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Moobot: the export is JSON, so the page parses it in the browser and posts
// the resulting manifest. There is no leg here on purpose, and that absence is
// the whole strategy: the raw file never crosses the wire. The posted manifest
// is untrusted like any other input and still goes through validateManifest
// (caps, lengths, perms) in the engine before anything renders or commits.
import { fileSourceInput, type ServerSourceStrategy } from '../strategy';

export const moobotSource: ServerSourceStrategy = {
  id: 'moobot',
  acceptInput: fileSourceInput
};
