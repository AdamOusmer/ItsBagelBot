// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CampaignContent, CampaignStatus, CampaignWire } from './server/campaigns';

export const CAMPAIGN_LOCALES = ['en', 'fr', 'es', 'pt-br', 'ru', 'de'] as const;

export type CampaignLocale = (typeof CAMPAIGN_LOCALES)[number];

export type CampaignDraftContent = Record<CampaignLocale, CampaignContent>;

export const CAMPAIGN_NAME_MAX = 120;
export const CAMPAIGN_SUBJECT_MAX = 200;
export const CAMPAIGN_BODY_MAX = 200 << 10;

export const CAMPAIGN_STATUS = {
  draft: { label: 'admin.campaigns.statusDraft', tone: 'neutral' },
  approved: { label: 'admin.campaigns.statusApproved', tone: 'warning' },
  sending: { label: 'admin.campaigns.statusSending', tone: 'warning' },
  paused: { label: 'admin.campaigns.statusPaused', tone: 'neutral' },
  done: { label: 'admin.campaigns.statusDone', tone: 'success' }
} as const satisfies Record<CampaignStatus, { label: string; tone: string }>;

export const SEND_LABEL = {
  queued: 'admin.campaigns.sendQueued',
  sent: 'admin.campaigns.sendSent',
  delivered: 'admin.campaigns.sendDelivered',
  bounced: 'admin.campaigns.sendBounced',
  complained: 'admin.campaigns.sendComplained',
  skipped: 'admin.campaigns.sendSkipped',
  failed: 'admin.campaigns.sendFailed'
} as const;

export type SendStatus = keyof typeof SEND_LABEL;

export const SEND_STATUSES = Object.keys(SEND_LABEL) as SendStatus[];

const DISPATCHED: readonly SendStatus[] = ['sent', 'delivered', 'bounced', 'complained'];

export function dispatchedCount(sends: Readonly<Partial<Record<string, number>>>): number {
  return DISPATCHED.reduce((total, status) => total + (sends[status] ?? 0), 0);
}

export function blankContent(): CampaignDraftContent {
  return Object.fromEntries(CAMPAIGN_LOCALES.map((locale) => [locale, { subject: '', html: '', text: '' }])) as CampaignDraftContent;
}

export function draftContent(saved: CampaignWire['content']): CampaignDraftContent {
  const content = blankContent();
  for (const locale of CAMPAIGN_LOCALES) content[locale] = { ...content[locale], ...saved?.[locale] };
  return content;
}

export function contentComplete(content: CampaignContent | undefined): boolean {
  return Boolean(content?.subject && content.html && content.text);
}

export function contentBlank(content: CampaignContent): boolean {
  return !content.subject && !content.html && !content.text;
}

export function contentPayload(content: CampaignDraftContent): Record<string, CampaignContent> {
  return Object.fromEntries(CAMPAIGN_LOCALES.filter((locale) => !contentBlank(content[locale])).map((locale) => [locale, content[locale]]));
}

function validContent(value: unknown): value is CampaignContent {
  if (!isRecord(value)) return false;
  return Object.keys(LIMITS).every((part) => typeof value[part] === 'string');
}

const LIMITS: Record<keyof CampaignContent, number> = { subject: CAMPAIGN_SUBJECT_MAX, html: CAMPAIGN_BODY_MAX, text: CAMPAIGN_BODY_MAX };

function withinLimits(content: CampaignContent): boolean {
  return (Object.keys(LIMITS) as (keyof CampaignContent)[]).every((part) => content[part].length <= LIMITS[part]);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function acceptable(content: unknown): content is CampaignContent {
  return validContent(content) && withinLimits(content);
}

function parseContent(raw: string): Record<string, CampaignContent> | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isRecord(parsed)) return null;
  const entries = Object.entries(parsed);
  const known = entries.every(([locale]) => (CAMPAIGN_LOCALES as readonly string[]).includes(locale));
  if (!known) return null;
  if (!entries.every(([, content]) => acceptable(content))) return null;
  return parsed as Record<string, CampaignContent>;
}

export function parseCampaignName(value: FormDataEntryValue | null): string | { error: string } {
  const name = String(value ?? '').trim();
  if (!name) return { error: 'campaign name required' };
  return name.slice(0, CAMPAIGN_NAME_MAX);
}

export type CampaignDraftForm = { name: string; content: Record<string, CampaignContent> };

export function parseCampaignDraft(form: FormData): CampaignDraftForm | { error: string } {
  const name = parseCampaignName(form.get('name'));
  if (typeof name !== 'string') return name;
  const content = parseContent(String(form.get('content') ?? '{}'));
  if (!content) return { error: 'campaign content invalid' };
  return { name, content };
}

export type CampaignCreatedPayload = { campaign?: { id?: number }; action?: { ok?: boolean } };
