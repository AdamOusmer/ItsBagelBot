// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DiscordConfig, PinnedSlot } from '@bagel/kit';
import type { I18n } from '@bagel/kit';

export type I18nKey = Parameters<I18n['t']>[0];

export const FIELD_LABEL_KEYS: Partial<Record<keyof DiscordConfig, I18nKey>> = {
  liveChannelId: 'discord.liveChannelLabel',
  clipsChannelId: 'discord.clipsChannelLabel',
  welcomeChannelId: 'discord.welcomeChannelLabel',
  voiceHubId: 'discord.voiceHubLabel',
  logChannelId: 'discord.logChannelLabel',
  subsChannelId: 'discord.subsChannelLabel',
  subsCategoryId: 'discord.subsCategoryLabel',
  vipChannelId: 'discord.vipChannelLabel',
  vipCategoryId: 'discord.vipCategoryLabel',
  ticketChannelId: 'discord.ticketChannelLabel',
  ticketCategoryId: 'discord.ticketCategoryLabel',
  ticketArchiveCategoryId: 'discord.ticketArchiveLabel',
  ticketLogChannelId: 'discord.ticketLogLabel',
  ticketStaffRoleIds: 'discord.staffRolesLabel',
  ticketOpenLimit: 'discord.openLimitLabel',
  ticketPanelTitle: 'discord.panelTitleLabel',
  ticketPanelBody: 'discord.panelBodyLabel',
  ticketPanelColor: 'discord.panelColorLabel',
  ticketPanelButton: 'discord.panelButtonLabel',
  ownerRoleId: 'discord.ownerRoleLabel',
  leadModRoleId: 'discord.leadModRoleLabel',
  modsRoleId: 'discord.modsRoleLabel',
  vipRoleId: 'discord.vipRoleLabel',
  subscriberRoleId: 'discord.subscriberRoleLabel',
  regularsRoleId: 'discord.regularsRoleLabel',
  memberRoleId: 'discord.memberRoleLabel',
  pinnedRoles: 'discord.pinnedChip',
  categoryAllow: 'discord.allowLabel',
  categoryDeny: 'discord.denyLabel',
  linkAllowList: 'discord.linkGuardLabel'
};

export const SLOT_LABEL_KEYS: Record<PinnedSlot, I18nKey> = {
  owner: 'discord.slotOwner',
  leadMod: 'discord.slotLeadMod',
  mods: 'discord.slotMods',
  vip: 'discord.slotVip',
  subscriber: 'discord.slotSubscriber',
  regulars: 'discord.slotRegulars',
  member: 'discord.slotMember'
};

export const CLOSE_KEYS: Record<
  number,
  'discord.close4004' | 'discord.close4013' | 'discord.close4014' | 'discord.close4011'
> = {
  4004: 'discord.close4004',
  4011: 'discord.close4011',
  4013: 'discord.close4013',
  4014: 'discord.close4014'
};

export const CHANNEL_FIELDS = [
  'liveChannelId',
  'clipsChannelId',
  'welcomeChannelId',
  'voiceHubId',
  'logChannelId',
  'subsChannelId',
  'subsCategoryId',
  'vipChannelId',
  'vipCategoryId'
] as const satisfies readonly (keyof DiscordConfig)[];

export const ROLE_FIELDS = [
  'ownerRoleId',
  'leadModRoleId',
  'modsRoleId',
  'vipRoleId',
  'subscriberRoleId',
  'regularsRoleId',
  'memberRoleId',
  'pinnedRoles',
  'autoRoleEnabled'
] as const satisfies readonly (keyof DiscordConfig)[];

export const ANNOUNCEMENT_FIELDS = [
  'liveEnabled',
  'clipsEnabled',
  'categoryAllow',
  'categoryDeny'
] as const satisfies readonly (keyof DiscordConfig)[];

export const COMMUNITY_FIELDS = [
  'welcomeEnabled',
  'goodbyeEnabled',
  'voiceEnabled',
  'logsEnabled',
  'levelsEnabled',
  'linkGuardEnabled',
  'linkAllowList',
  'subscribersEnabled'
] as const satisfies readonly (keyof DiscordConfig)[];

export const TICKET_FIELDS = [
  'ticketsEnabled',
  'ticketChannelId',
  'ticketCategoryId',
  'ticketArchiveCategoryId',
  'ticketLogChannelId',
  'ticketStaffRoleIds',
  'ticketOpenLimit',
  'ticketTranscriptEnabled',
  'ticketPanelTitle',
  'ticketPanelBody',
  'ticketPanelColor',
  'ticketPanelButton'
] as const satisfies readonly (keyof DiscordConfig)[];
