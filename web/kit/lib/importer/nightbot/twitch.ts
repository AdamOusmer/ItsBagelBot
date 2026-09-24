// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { emit } from '../targets';
import type { Concept } from '../targets';
import { mappedSpans } from '../validate';
import type { Token } from './scan';
import { literal } from './variables';
import type { TokenResult } from './variables';

const TWITCH_FIELDS: Record<string, { concept: Concept; otherChannel: boolean }> = {
  title: { concept: 'title', otherChannel: true },
  game: { concept: 'game', otherChannel: true },
  uptimelength: { concept: 'uptime', otherChannel: true },
  viewers: { concept: 'channel.viewers', otherChannel: false },
  followers: { concept: 'followers', otherChannel: false },
  subscribercount: { concept: 'subs', otherChannel: false }
};

const TWITCH_CALL = /^\s+("[^"]*"|\S+)\s+"([^"]*)"\s*$/;

const TWITCH_FIELD_REF = /\{\{(\w+)\}\}/g;

function unquote(s: string): string {
  return s.startsWith('"') && s.endsWith('"') ? s.slice(1, -1) : s;
}

function fieldUnavailable(field: string, ownChannel: boolean): boolean {
  const rule = TWITCH_FIELDS[field.toLowerCase()];
  if (!rule) return true;
  return !ownChannel && !rule.otherChannel;
}

function substituteFields(
  fmt: string,
  ownChannel: boolean,
  channelArg: string
): { repl: string; minted: string[]; anyUnmapped: boolean } {
  const minted: string[] = [];
  let anyUnmapped = false;
  const repl = fmt.replace(TWITCH_FIELD_REF, (raw: string, field: string) => {
    if (fieldUnavailable(field, ownChannel)) {
      anyUnmapped = true;
      return raw;
    }
    const rule = TWITCH_FIELDS[field.toLowerCase()];
    const span = ownChannel ? emit(rule.concept) : emit(rule.concept, channelArg);
    if (span === null) {
      anyUnmapped = true;
      return raw;
    }
    minted.push(span);
    return span;
  });
  return { repl, minted, anyUnmapped };
}

function spansSurvive(text: string, minted: string[]): boolean {
  const found = mappedSpans(text).map((t) => t.raw);
  for (const span of minted) {
    const at = found.indexOf(span);
    if (at === -1) return false;
    found.splice(at, 1);
  }
  return true;
}

export function twitchToken(token: Token): TokenResult | null {
  if (token.head !== 'twitch') return null;
  const m = TWITCH_CALL.exec(token.rest);
  if (!m) return literal(token);
  const channelArg = unquote(m[1]);
  const ownChannel = channelArg === emit('channel');
  const { repl, minted, anyUnmapped } = substituteFields(m[2], ownChannel, channelArg);
  if (anyUnmapped) return { repl, warned: true };
  if (!spansSurvive(repl, minted)) return literal(token);
  return { repl, warned: false };
}
