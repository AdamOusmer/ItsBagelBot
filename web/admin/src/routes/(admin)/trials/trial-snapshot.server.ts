// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { trialList } from '#lib/server/services';

export const trialDemo = dev && process.env.DEMO === '1';

export function readTrialSnapshot() {
  return trialDemo ? import('#lib/server/demo-data').then(({ sampleTrials }) => sampleTrials) : trialList();
}
