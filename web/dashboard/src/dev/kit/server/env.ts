// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { demoMailingAddress } from '../../fixtures';

const origin = process.env.ORIGIN ?? 'http://localhost:5173';

export const env: Record<string, string | undefined> = {
  ORIGIN: origin,
  BUSINESS_MAILING_ADDRESS: demoMailingAddress,
  SPOTIFY_REDIRECT_URI: `${origin}/spotify/callback`,
  ...process.env
};
