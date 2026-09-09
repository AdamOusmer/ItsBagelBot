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
//	$(uptime) $(current_game)   → literal + warn (no live-stream tokens here)
//	$(current_status) $(cmd_count) → literal + warn (reads upstream state)
//	$currency(…) $arg(n) $args(…) → literal + warn (no argument slicing here)
//	[b]…[/b]                    → the text, unwrapped
//
// $(nick) and $(display_name) deliberately collapse onto the same token: this
// bot substitutes one user token and it renders the display name, so keeping
// them apart would mean inventing a second token that resolves identically.
//
// $arg(n)/$args(n-) are NOT folded onto {args}: they slice one word (or a
// range) out of what the viewer typed, and handing over the WHOLE argument
// string would silently change what the command answers rather than translate
// it. Literal plus a warning lets the broadcaster see the tag in review and
// decide.
//
// Observation, 2026-09-07: across the 365 rows read off two public streaming
// websites, not one response carried a $(…) tag at all. The published list
// shows resolved public text, so this table is the documented contract rather
// than something the fixtures exercise; the synthetic rows in the fixture are
// what keep it honest.

// SIMPLE_TAGS maps a bare $(name) whose body carries no arguments.
const SIMPLE_TAGS: Record<string, string> = {
  nick: '{user}',
  display_name: '{user}',
  channel_name: '{channel}',
  message_clear: '{args}'
};

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
}

// mapTag resolves one matched tag, or returns null when it has no equivalent
// and must stay literal.
function mapTag(head: string, body: string): string | null {
  if (head !== '') return null;
  return SIMPLE_TAGS[body.trim().toLowerCase()] ?? null;
}

// translateTags rewrites one decoded Wizebot response. Entities are already
// gone by the time this runs (./entities), so a tag never hides behind
// "$&#40;nick&#41;".
export function translateTags(input: string): TagTranslation {
  const seen = new Set<string>();
  const warns: string[] = [];
  const text = input.replace(BOLD, '').replace(TAG, (raw: string, head: string, body: string) => {
    const mapped = mapTag(head, body);
    if (mapped !== null) return mapped;
    if (!seen.has(raw)) {
      seen.add(raw);
      warns.push(raw);
    }
    return raw;
  });
  return { text, warns };
}
