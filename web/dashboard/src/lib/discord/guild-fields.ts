// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DiscordConfig, PinnedSlot } from '@bagel/kit';
import type { I18n } from '@bagel/kit';

export type I18nKey = Parameters<I18n['t']>[0];

export const FIELD_LABEL_KEYS: Partial<Record<keyof DiscordConfig, I18nKey>> = {
  liveChannelId: 'discord.channels.liveLabel',
  clipsChannelId: 'discord.channels.clipsLabel',
  welcomeChannelId: 'discord.channels.welcomeLabel',
  voiceHubId: 'discord.channels.voiceHubLabel',
  logChannelId: 'discord.channels.logLabel',
  subsChannelId: 'discord.channels.subsLabel',
  subsCategoryId: 'discord.channels.subsCategoryLabel',
  vipChannelId: 'discord.channels.vipLabel',
  vipCategoryId: 'discord.channels.vipCategoryLabel',
  ticketChannelId: 'discord.tickets.channelLabel',
  ticketCategoryId: 'discord.tickets.categoryLabel',
  ticketArchiveCategoryId: 'discord.tickets.archiveLabel',
  ticketLogChannelId: 'discord.tickets.logLabel',
  ticketStaffRoleIds: 'discord.tickets.staffRolesLabel',
  ticketOpenLimit: 'discord.tickets.openLimitLabel',
  ticketPanelTitle: 'discord.tickets.panelTitleLabel',
  ticketPanelBody: 'discord.tickets.panelBodyLabel',
  ticketPanelColor: 'discord.tickets.panelColorLabel',
  ticketPanelButton: 'discord.tickets.panelButtonLabel',
  ownerRoleId: 'discord.roles.ownerLabel',
  leadModRoleId: 'discord.roles.leadModLabel',
  modsRoleId: 'discord.roles.modsLabel',
  vipRoleId: 'discord.roles.vipLabel',
  subscriberRoleId: 'discord.roles.subscriberLabel',
  regularsRoleId: 'discord.roles.regularsLabel',
  memberRoleId: 'discord.roles.memberLabel',
  pinnedRoles: 'discord.roles.pinnedChip',
  categoryAllow: 'discord.announcements.allowLabel',
  categoryDeny: 'discord.announcements.denyLabel',
  linkAllowList: 'discord.community.linkGuardLabel'
};

export const SLOT_LABEL_KEYS: Record<PinnedSlot, I18nKey> = {
  owner: 'discord.roles.slotOwner',
  leadMod: 'discord.roles.slotLeadMod',
  mods: 'discord.roles.slotMods',
  vip: 'discord.roles.slotVip',
  subscriber: 'discord.roles.slotSubscriber',
  regulars: 'discord.roles.slotRegulars',
  member: 'discord.roles.slotMember'
};

export const CLOSE_KEYS: Record<
  number,
  'discord.status.close4004' | 'discord.status.close4013' | 'discord.status.close4014' | 'discord.status.close4011'
> = {
  4004: 'discord.status.close4004',
  4011: 'discord.status.close4011',
  4013: 'discord.status.close4013',
  4014: 'discord.status.close4014'
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
