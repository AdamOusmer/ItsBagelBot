// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function isBotAccount(
  userId: string,
  config: Readonly<Record<string, string | undefined>>
): boolean {
  const botId = config.TWITCH_BOT_USER_ID?.trim() || config.ADMIN_BOT_USER_ID?.trim();
  return !!botId && userId === botId;
}
