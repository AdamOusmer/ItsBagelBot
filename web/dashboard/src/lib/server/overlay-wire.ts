// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// internal/domain/automation/overlay.go writes these; change both together.
const CHANNEL_PREFIX = 'overlay:';
const WIRE_VERSION = 1;
const MAX_MESSAGE_CHARS = 8192;
const MAX_URL_CHARS = 2000;
const MAX_SECONDS = 60;
const MAX_VOLUME = 100;

export type OverlayPosition = 'top' | 'center' | 'bottom';

export type OverlayItem =
  | { kind: 'text' | 'image' | 'video'; content: string; seconds: number; position: OverlayPosition }
  | { kind: 'sound'; content: string; volume: number };

export const OVERLAY_REVOKE_MESSAGE = JSON.stringify({ v: WIRE_VERSION, kind: 'revoke' });

export function overlayChannel(channelId: string): string {
  return CHANNEL_PREFIX + channelId;
}

export function overlayChannelId(channel: string): string | null {
  return channel.startsWith(CHANNEL_PREFIX) ? channel.slice(CHANNEL_PREFIX.length) : null;
}

export function overlayMediaUrl(raw: string): boolean {
  if (raw.length > MAX_URL_CHARS) return false;
  try {
    const url = new URL(raw);
    return url.protocol === 'https:' && url.hostname !== '' && url.username === '' && url.password === '';
  } catch {
    return false;
  }
}

function bounded(value: unknown, low: number, high: number, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? Math.min(high, Math.max(low, Math.trunc(value))) : fallback;
}

function position(value: unknown): OverlayPosition {
  return value === 'top' || value === 'bottom' ? value : 'center';
}

type Message = { v?: unknown; kind?: unknown; content?: unknown; seconds?: unknown; position?: unknown; volume?: unknown };

function shown(kind: 'text' | 'image' | 'video', content: string, msg: Message): OverlayItem {
  return { kind, content, seconds: bounded(msg.seconds, 1, MAX_SECONDS, 5), position: position(msg.position) };
}

function itemFrom(msg: Message): OverlayItem | null {
  const { kind, content } = msg;
  if (typeof content !== 'string') return null;
  if (kind === 'text') return shown(kind, content, msg);
  if (!overlayMediaUrl(content)) return null;
  if (kind === 'sound') return { kind, content, volume: bounded(msg.volume, 0, MAX_VOLUME, MAX_VOLUME) };
  return kind === 'image' || kind === 'video' ? shown(kind, content, msg) : null;
}

function decode(raw: string): Message | null {
  if (raw.length > MAX_MESSAGE_CHARS) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? (parsed as Message) : null;
  } catch {
    return null;
  }
}

export function readOverlayMessage(raw: string): OverlayItem | 'revoke' | null {
  const msg = decode(raw);
  if (msg?.v !== WIRE_VERSION) return null;
  return msg.kind === 'revoke' ? 'revoke' : itemFrom(msg);
}

export function overlayItemFrame(item: OverlayItem): string {
  return `event: item\ndata: ${JSON.stringify(item)}\n\n`;
}
