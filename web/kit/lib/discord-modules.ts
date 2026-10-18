// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  VOICE_PRIVACY_MODES,
  blankDiscordConfig,
  mergeDiscordConfig,
  type DiscordConfig,
  type FieldError
} from './discord-config';
import { tCatalog } from './module-copy';
import type { MessageKey } from './i18n/keys';

type TFn = (key: MessageKey, params?: Record<string, string | number>) => string;

export type DiscordModuleKind = 'core' | 'default' | 'optin';

export type DiscordModuleFieldType =
  | 'text'
  | 'textarea'
  | 'number'
  | 'select'
  | 'toggle'
  | 'channel'
  | 'category'
  | 'role'
  | 'roleList'
  | 'channelList'
  | 'nameList'
  | 'color'
  | 'pinnedRoles';

export type DiscordModuleOption = { value: string; label: string };

export interface DiscordModuleField {
  key: keyof DiscordConfig;
  label: string;
  type: DiscordModuleFieldType;
  placeholder?: string;
  help?: string;
  options?: DiscordModuleOption[];
  defaultOn?: boolean;
}

export interface DiscordModuleDef {
  id: string;
  kind: DiscordModuleKind;
  label: string;
  tagline: string;
  fields: DiscordModuleField[];
  href?: string;
}

function field(
  key: keyof DiscordConfig,
  label: string,
  type: DiscordModuleFieldType,
  rest: Partial<DiscordModuleField> = {}
): DiscordModuleField {
  return { key, label, type, ...rest };
}

const channel = (key: keyof DiscordConfig, label: string) => field(key, label, 'channel');
const category = (key: keyof DiscordConfig, label: string) => field(key, label, 'category');
const role = (key: keyof DiscordConfig, label: string) => field(key, label, 'role');
const toggle = (key: keyof DiscordConfig, label: string, defaultOn: boolean) =>
  field(key, label, 'toggle', { defaultOn });

const PRIVACY_LABELS: Record<(typeof VOICE_PRIVACY_MODES)[number], string> = {
  open: 'Open',
  locked: 'Locked',
  hidden: 'Hidden'
};

export const DISCORD_MODULES: readonly DiscordModuleDef[] = [
  {
    id: 'roles',
    kind: 'core',
    label: 'Roles',
    tagline: 'Which roles count as owner, moderators, VIPs and subscribers.',
    fields: [
      role('ownerRoleId', 'Owner role'),
      role('leadModRoleId', 'Lead moderator role'),
      role('modsRoleId', 'Moderator role'),
      role('vipRoleId', 'VIP role'),
      role('subscriberRoleId', 'Subscriber role'),
      role('regularsRoleId', 'Regulars role'),
      field('pinnedRoles', 'Pinned roles', 'pinnedRoles')
    ]
  },
  {
    id: 'live',
    kind: 'default',
    label: 'Live alerts',
    tagline: 'Announce when the stream goes live.',
    fields: [
      channel('liveChannelId', 'Live channel'),
      field('categoryAllow', 'Only these categories', 'nameList'),
      field('categoryDeny', 'Never these categories', 'nameList'),
      field('twitchLogin', 'Twitch channel', 'text')
    ]
  },
  {
    id: 'clips',
    kind: 'default',
    label: 'Clips',
    tagline: 'Share new Twitch clips.',
    fields: [channel('clipsChannelId', 'Clips channel')]
  },
  {
    id: 'welcome',
    kind: 'default',
    label: 'Welcome',
    tagline: 'Greet new members and say goodbye.',
    fields: [channel('welcomeChannelId', 'Welcome channel'), toggle('goodbyeEnabled', 'Say goodbye', false)]
  },
  {
    id: 'autorole',
    kind: 'default',
    label: 'Auto role',
    tagline: 'Give every new member a role.',
    fields: [role('memberRoleId', 'Member role')]
  },
  {
    id: 'subscribers',
    kind: 'optin',
    label: 'Subscriber rooms',
    tagline: 'Private rooms for subscribers and VIPs.',
    fields: [
      channel('subsChannelId', 'Subscriber channel'),
      category('subsCategoryId', 'Subscriber category'),
      channel('vipChannelId', 'VIP channel'),
      category('vipCategoryId', 'VIP category')
    ]
  },
  {
    id: 'voice',
    kind: 'default',
    label: 'Voice rooms',
    tagline: 'Join a hub to get your own voice room.',
    fields: [
      channel('voiceHubId', 'Hub channel'),
      category('voiceCategoryId', 'Room category'),
      field('voiceNameTemplate', 'Room name', 'text'),
      field('voiceUserLimit', 'User limit', 'number'),
      field('voicePrivacy', 'Privacy', 'select', {
        options: VOICE_PRIVACY_MODES.map((value) => ({ value, label: PRIVACY_LABELS[value] }))
      })
    ]
  },
  {
    id: 'tickets',
    kind: 'default',
    label: 'Tickets',
    tagline: 'A private help desk with staff and transcripts.',
    href: 'tickets',
    fields: [
      channel('ticketChannelId', 'Panel channel'),
      category('ticketCategoryId', 'Ticket category'),
      category('ticketArchiveCategoryId', 'Archive category'),
      field('ticketStaffRoleIds', 'Staff roles', 'roleList'),
      field('ticketOpenLimit', 'Open tickets per member', 'number'),
      toggle('ticketTranscriptEnabled', 'Save transcripts', true),
      channel('ticketLogChannelId', 'Ticket log channel'),
      field('ticketPanelTitle', 'Panel title', 'text'),
      field('ticketPanelBody', 'Panel text', 'textarea'),
      field('ticketPanelColor', 'Panel color', 'color'),
      field('ticketPanelButton', 'Button label', 'text')
    ]
  },
  {
    id: 'logs',
    kind: 'default',
    label: 'Server logs',
    tagline: 'Record what happens in the server.',
    fields: [
      channel('logChannelId', 'Log channel'),
      toggle('logMessagesEnabled', 'Message logs', true),
      toggle('logMembersEnabled', 'Member logs', true),
      toggle('logVoiceEnabled', 'Voice logs', true),
      toggle('logModerationEnabled', 'Moderation logs', true),
      toggle('logChannelsEnabled', 'Channel logs', true),
      toggle('logRolesEnabled', 'Role logs', true),
      toggle('logServerEnabled', 'Server logs', true),
      toggle('logIgnoreBots', 'Ignore bots', true),
      channel('logMessagesChannelId', 'Message log channel'),
      channel('logMembersChannelId', 'Member log channel'),
      channel('logVoiceChannelId', 'Voice log channel'),
      channel('logModerationChannelId', 'Moderation log channel'),
      field('logIgnoredChannelIds', 'Ignored channels', 'channelList')
    ]
  },
  {
    id: 'levels',
    kind: 'default',
    label: 'Levels',
    tagline: 'Members earn XP and levels by chatting.',
    fields: []
  },
  {
    id: 'linkguard',
    kind: 'optin',
    label: 'Link guard',
    tagline: 'Remove links from members unless allowed.',
    fields: [field('linkAllowList', 'Allowed links', 'textarea')]
  },
  {
    id: 'moderation',
    kind: 'default',
    label: 'Moderation',
    tagline: 'Moderation commands for staff.',
    fields: []
  }
];

const BY_ID: ReadonlyMap<string, DiscordModuleDef> = new Map(DISCORD_MODULES.map((def) => [def.id, def]));

export function discordModuleById(id: string): DiscordModuleDef | undefined {
  return BY_ID.get(id);
}

export function discordModuleToggleable(def: Pick<DiscordModuleDef, 'kind'>): boolean {
  return def.kind !== 'core';
}

export function discordToggleOn(f: Pick<DiscordModuleField, 'defaultOn'>, value: string | undefined): boolean {
  return f.defaultOn === false ? value === 'on' : value !== 'off';
}

export function discordModuleKey(id: string, ...parts: string[]): MessageKey {
  return ['discord', 'modules', id, ...parts].join('.') as MessageKey;
}

export function tDiscordModuleLabel(t: TFn, def: Pick<DiscordModuleDef, 'id' | 'label'>): string {
  return tCatalog(t, discordModuleKey(def.id, 'label'), def.label);
}

export function tDiscordModuleTagline(t: TFn, def: Pick<DiscordModuleDef, 'id' | 'tagline'>): string {
  return tCatalog(t, discordModuleKey(def.id, 'tagline'), def.tagline);
}

export function tDiscordModuleField(
  t: TFn,
  moduleId: string,
  f: DiscordModuleField,
  part: 'label' | 'help' | 'placeholder'
): string {
  return tCatalog(t, discordModuleKey(moduleId, 'fields', f.key, part), f[part] ?? '');
}

export function tDiscordModuleOption(t: TFn, moduleId: string, fieldKey: string, opt: DiscordModuleOption): string {
  return tCatalog(t, discordModuleKey(moduleId, 'fields', fieldKey, 'options', opt.value), opt.label);
}

export type DiscordModuleSettings = Record<string, string>;

export type DiscordModuleValidation = { settings: DiscordModuleSettings; errors: FieldError[] };

export function discordModuleKeys(def: DiscordModuleDef): (keyof DiscordConfig)[] {
  return def.fields.map((f) => f.key);
}

/** Null for an unknown module id. Keys absent from `patch` keep their `current` value. */
export function validateDiscordModule(
  id: string,
  patch: Record<string, unknown>,
  current: DiscordModuleSettings = {}
): DiscordModuleValidation | null {
  const def = BY_ID.get(id);
  if (!def) return null;
  const keys = discordModuleKeys(def);
  const base = blankDiscordConfig();
  const scoped: Record<string, unknown> = {};
  for (const key of keys) {
    base[key] = current[key] ?? '';
    scoped[key] = patch[key];
  }
  const { config, errors } = mergeDiscordConfig(base, scoped);
  const settings: DiscordModuleSettings = {};
  for (const key of keys) settings[key] = config[key];
  return { settings, errors };
}

export function parseDiscordModuleSettings(id: string, raw: unknown): DiscordModuleSettings {
  const def = BY_ID.get(id);
  const src = raw !== null && typeof raw === 'object' && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
  const out: DiscordModuleSettings = {};
  for (const key of def ? discordModuleKeys(def) : []) {
    const v = src[key];
    out[key] = typeof v === 'string' ? v : '';
  }
  return out;
}
