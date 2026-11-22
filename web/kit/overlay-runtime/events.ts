// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { alertKinds, type AlertKind, type FeedEvent, type Identity, type OverlayEvent } from './feed';

const alertSet = new Set<string>(alertKinds);
const ANONYMOUS = 'Anonymous';

export const isAlert = (kind: OverlayEvent['kind']): kind is AlertKind => alertSet.has(kind);
export const eventAmount = (event: OverlayEvent): number => event.amount ?? Number(event.kind === 'gift_sub');

export function displayName(user?: Identity): string {
  return user?.name || user?.login || ANONYMOUS;
}

export function tokens(event: OverlayEvent): Record<string, string | number> {
  return {
    name: event.anonymous ? ANONYMOUS : displayName(event.user),
    amount: event.amount ?? 0,
    tier: event.tier ?? '',
    months: event.months ?? 0,
    streak: event.streak ?? 0,
    count: eventAmount(event),
    recipient: event.recipient ? displayName(event.recipient) : '',
    message: event.message?.text ?? '',
    reward: event.reward?.title ?? ''
  };
}

export function renderTemplate(template: string, data: Record<string, string | number>): string {
  return template.replace(/\{([a-z_]+)\}/g, (token, key: string) => (Object.hasOwn(data, key) ? String(data[key]) : token));
}

export function emoteURL(id?: string): string | undefined {
  if (!id || !/^[a-zA-Z0-9_-]+$/.test(id)) return;
  return `https://static-cdn.jtvnw.net/emoticons/v2/${id}/default/dark/2.0`;
}

export const eventKey = (event: FeedEvent): string => (event.replay || event.test ? String(event.seq) : event.id);
