// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { emit, positional, slice } from '../targets';

const SIMPLE_TAGS: Record<string, string> = {
  nick: emit('user')!,
  display_name: emit('user')!,
  channel_name: emit('channel')!,
  message_clear: emit('args')!,
  uptime: emit('uptime')!,
  current_game: emit('game')!,
  current_status: emit('title')!,
  cmd_count: emit('count')!
};

const ARG_TAG = /^[0-9]+$/;
const ARGS_RANGE_TAG = /^([0-9]+)-$/;

const TAG = /\$([A-Za-z_]*)\(([^()]*)\)/g;

const BOLD = /\[\/?b\]/gi;

export interface TagTranslation {
  text: string;
  warns: string[];
  countRemapped: boolean;
}

function mapTag(head: string, body: string): string | null {
  const trimmed = body.trim();
  if (head === '') return SIMPLE_TAGS[trimmed.toLowerCase()] ?? null;
  if (head.toLowerCase() === 'arg') return ARG_TAG.test(trimmed) ? positional(Number(trimmed)) : null;
  if (head.toLowerCase() === 'args') {
    const m = ARGS_RANGE_TAG.exec(trimmed);
    return m ? slice(Number(m[1])) : null;
  }
  return null;
}

export function translateTags(input: string): TagTranslation {
  const seen = new Set<string>();
  const warns: string[] = [];
  let countRemapped = false;
  const text = input.replace(BOLD, '').replace(TAG, (raw: string, head: string, body: string) => {
    const mapped = mapTag(head, body);
    if (mapped !== null) {
      if (head === '' && body.trim().toLowerCase() === 'cmd_count') countRemapped = true;
      return mapped;
    }
    if (!seen.has(raw)) {
      seen.add(raw);
      warns.push(raw);
    }
    return raw;
  });
  return { text, warns, countRemapped };
}
