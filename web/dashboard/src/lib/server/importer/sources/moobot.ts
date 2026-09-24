// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fileSourceInput, type ServerSourceStrategy } from '../strategy';

export const moobotSource: ServerSourceStrategy = {
  id: 'moobot',
  acceptInput: fileSourceInput
};
