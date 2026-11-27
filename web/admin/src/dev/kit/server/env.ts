// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const env: Record<string, string | undefined> = {
  TWITCH_BOT_USER_ID: 'demo-bot',
  DASHBOARD_PUBLIC_ORIGIN: 'http://localhost:5173',
  ...process.env
};
