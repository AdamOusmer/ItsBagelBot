// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The typed literal maps the guild pages share.
//
// They live in one module rather than in each page because a field name has to
// mean the same thing in every one of them: `save` merges a partial draft and
// answers with WIRE field names, so a refusal raised by the Channels form can
// name a field the Tickets page owns. One table is what lets any page label any
// refused field; seven copies would drift the first time a field is renamed.
import type { DiscordConfig, PinnedSlot } from '@bagel/shared';
import type { I18n } from '@bagel/shared';

export type I18nKey = Parameters<I18n['t']>[0];

/**
 * A refused field, named the way the pages name it.
 *
 * The action reports wire field names (`ticketStaffRoleIds`), which are the one
 * thing these pages have spent their whole existence not showing anybody. The
 * map is a typed literal so the i18n scanner sees the keys; a field missing
 * from it degrades to its own name rather than to nothing.
 */
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

// Only the fatal close codes get their own sentence: they are the ones the
// streamer can act on. Everything else is a transient disconnect the gateway
// retries on its own, so it reads as "reconnecting".
export const CLOSE_KEYS: Record<
  number,
  'discord.close4004' | 'discord.close4013' | 'discord.close4014' | 'discord.close4011'
> = {
  4004: 'discord.close4004',
  4011: 'discord.close4011',
  4013: 'discord.close4013',
  4014: 'discord.close4014'
};

/**
 * The slice of DiscordConfig each sub-page owns.
 *
 * A page posts only these keys, and `save` runs them through
 * mergeDiscordConfig, whose contract is that absent keys keep their stored
 * value. That is what makes seven small forms safe where the old single page
 * posted the whole config from every one of its sections: two people editing
 * two different sub-pages no longer overwrite each other with a draft neither
 * of them touched.
 */
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
