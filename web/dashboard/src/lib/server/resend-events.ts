// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Delivery, DeliveryEvent, Suppression, SuppressionReason } from '#lib/server/services';

type EventData = {
  email_id?: string;
  to?: unknown;
  tags?: unknown;
  bounce?: { type?: string };
};

export type ResendEvent = { type: string; data: EventData };
export type EventPlan = { suppressions: Suppression[]; delivery: Delivery | null };

const MARKETING = { name: 'category', value: 'marketing' };
const ANGLE_ADDRESS = /<([^<>]+)>/;

const SUPPRESSION: Record<string, (data: EventData) => SuppressionReason | null> = {
  'email.bounced': (data) => (data.bounce?.type === 'Permanent' ? 'hard_bounce' : null),
  'email.complained': () => 'complaint',
  'email.suppressed': () => 'hard_bounce'
};

const DELIVERY: Record<string, DeliveryEvent> = {
  'email.delivered': 'delivered',
  'email.bounced': 'bounced',
  'email.complained': 'complained'
};

export function parseResendEvent(body: string): ResendEvent | null {
  try {
    const parsed = JSON.parse(body) as { type?: unknown; data?: unknown };
    if (typeof parsed.type !== 'string') return null;
    const data = parsed.data && typeof parsed.data === 'object' ? (parsed.data as EventData) : {};
    return { type: parsed.type, data };
  } catch {
    return null;
  }
}

function tagPairs(tags: unknown): [string, unknown][] {
  if (Array.isArray(tags)) return tags.map((tag) => [tag?.name, tag?.value]);
  return tags && typeof tags === 'object' ? Object.entries(tags) : [];
}

function marketingTagged(tags: unknown): boolean {
  return tagPairs(tags).some(([name, value]) => name === MARKETING.name && value === MARKETING.value);
}

function recipients(to: unknown): string[] {
  const list = Array.isArray(to) ? to : [to];
  return list
    .filter((entry): entry is string => typeof entry === 'string')
    .map((entry) => (ANGLE_ADDRESS.exec(entry)?.[1] ?? entry).trim())
    .filter(Boolean);
}

function deliveryFor(type: string, data: EventData): Delivery | null {
  const event = DELIVERY[type];
  const resendId = data.email_id;
  if (!event || !resendId) return null;
  return marketingTagged(data.tags) ? { resend_id: resendId, event } : null;
}

export function planFor({ type, data }: ResendEvent): EventPlan {
  const reason = SUPPRESSION[type]?.(data) ?? null;
  return {
    suppressions: reason ? recipients(data.to).map((email) => ({ email, reason })) : [],
    delivery: deliveryFor(type, data)
  };
}
