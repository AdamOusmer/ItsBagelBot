// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Tag layer of the Wizebot parser: Wizebot's own placeholder syntax translated
// into this bot's {key} substitutions.
//
// Decision record - Wizebot tag table (support.wizebot.tv/docs/tags):
//
//	$(nick) / $(display_name)   → {user}     (login vs. cased name; one token here)
//	$(channel_name)             → {channel}
//	$(message_clear)            → {args}     (the message minus the command word)
//	$(uptime)                   → {uptime}
//	$(current_game)             → {game}
//	$(current_status)           → {title}
//	$(cmd_count)                → {count}    (per-command run count, the {uses} alias)
//	$arg(n)                     → {n}        (phase 6: one positional word)
//	$args(n-)                   → {n:}       (phase 6: word n through the end)
//	$currency(…)                → literal + warn (no loyalty-currency read here)
//	[b]…[/b]                    → the text, unwrapped
//
// $(nick) and $(display_name) deliberately collapse onto the same token: this
// bot substitutes one user token and it renders the display name, so keeping
// them apart would mean inventing a second token that resolves identically.
//
// $arg(n)/$args(n-) map onto the same positional grammar every other source's
// $argN family does (see streamlabs-desktop/parameters.ts's decision record):
// $arg(n) is one word, $args(n-) is the rest from word n, and neither folds
// onto the whole {args} string, which would silently hand over words the
// viewer typed before word n too.
//
// Observation, 2026-09-07: across the 365 rows read off two public streaming
// websites, not one response carried a $(…) tag at all. The published list
// shows resolved public text, so this table is the documented contract rather
// than something the fixtures exercise; the synthetic rows in the fixture are
// what keep it honest.

import { emit, positional, slice } from '../targets';

// SIMPLE_TAGS maps a bare $(name) whose body carries no arguments.
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

// ARG_TAG/ARGS_RANGE_TAG read $arg(n) / $args(n-)'s numeric body. Both are
// plain digits; ARGS_RANGE_TAG additionally demands the trailing '-' that
// marks it as a range rather than a single word.
const ARG_TAG = /^[0-9]+$/;
const ARGS_RANGE_TAG = /^([0-9]+)-$/;

// TAG matches both spellings Wizebot uses: "$(name)" and "$name(args)". The
// body excludes parentheses, so the innermost call of a nested tag is what
// matches, and a lone "$" or an unclosed "$(" is left alone.
const TAG = /\$([A-Za-z_]*)\(([^()]*)\)/g;

// BOLD is Wizebot's only markup in command text. This bot posts plain chat
// lines, so the wrapper goes and the text stays; that is not lossy enough to
// warn about.
const BOLD = /\[\/?b\]/gi;

export interface TagTranslation {
  text: string;
  // warns lists each distinct tag that could not be mapped, in first-seen
  // order, spelled exactly as it appeared.
  warns: string[];
  // countRemapped is true when the response used $(cmd_count) at least once
  // (review: SLCB's $count and Moobot's <counter> both warn on this same
  // remap, and this tag was the one place in this file that mapped a
  // meaning-changing token without it). $(cmd_count) is Wizebot's per-command
  // run count; {count} (the {uses} alias) counts THIS bot's own runs from
  // zero, so a broadcaster reading the translated response is reading a
  // different number than the one Wizebot showed, even though the sentence
  // around it is unchanged.
  countRemapped: boolean;
}

// mapTag resolves one matched tag, or returns null when it has no equivalent
// and must stay literal.
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

// translateTags rewrites one decoded Wizebot response. Entities are already
// gone by the time this runs (./entities), so a tag never hides behind
// "$&#40;nick&#41;".
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
