// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { I18n, PinnedSlot } from '@bagel/kit';

export type I18nKey = Parameters<I18n['t']>[0];

export const SLOT_LABEL_KEYS: Readonly<Record<PinnedSlot, I18nKey>> = Object.freeze({
  owner: 'discord.roles.slotOwner',
  leadMod: 'discord.roles.slotLeadMod',
  mods: 'discord.roles.slotMods',
  vip: 'discord.roles.slotVip',
  subscriber: 'discord.roles.slotSubscriber',
  regulars: 'discord.roles.slotRegulars',
  member: 'discord.roles.slotMember'
});

export const CLOSE_KEYS: Readonly<Record<
  number,
  'discord.status.close4004' | 'discord.status.close4013' | 'discord.status.close4014' | 'discord.status.close4011'
>> = Object.freeze({
  4004: 'discord.status.close4004',
  4011: 'discord.status.close4011',
  4013: 'discord.status.close4013',
  4014: 'discord.status.close4014'
});
