// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const TRIGGERS_MODULE: ModuleDef =
{
  id: 'triggers',
  label: 'Trigger Words',
  tagline: 'Auto-reply when a word shows up in chat, no "!" needed.',
  description:
    'Give the bot a list of words or phrases and the line to post when it sees one in ordinary chat, no command prefix required. Each rule pairs a phrase with a response: pick how the phrase matches (whole word, contains, exact message or starts with; the default matches the whole word, so "hi" will not fire inside "this"), write the reply, and switch individual rules on or off without deleting them. Responses support {user}, {random} and {choice:a,b,c}. The first matching rule wins, so one message gets at most one reply.',
  category: 'Chat',
  defaultEnabled: false,
  // Trigger rules are a free-form list of phrase→response pairs the author grows,
  // so the module page renders them as add/removable ReplyRows with a bespoke
  // rule inspector (TriggerRuleEditor: phrase, match mode, response, per-rule
  // enabled switch). The whole list is persisted as one "rules" string holding
  // a JSON array of structured rules; the sesame module also still parses the
  // legacy "phrase => response" line format for configs saved before the
  // migration. See app/twitch/sesame/modules/triggers.go.
  //
  // No ModuleReply here, unlike channelpoints/govee/songqueue above: a
  // trigger response substitutes {user}, {random} and {choice:a,b,c}
  // (module.ParseDynamic + {user}), and those are not module-private
  // substitution names the way {reward} or {track} are — they are three
  // ordinary custom-command Variables already in the manifest (variables.ts),
  // complete with real forms, samples and locale copy. TriggerRuleEditor's
  // insert palette (docs/specs/variables-catalog.md phase 4) reads
  // forSurface('triggers') in surfaces.ts, which picks those three manifest
  // entries by id instead of inventing a parallel ReplyToken palette: a
  // ModuleReply.tokens entry only stores a bare name -> sample pair, which
  // cannot carry {choice}'s worked payload example ({choice:yes,no,maybe}),
  // so reusing the manifest is strictly more correct here, not just shorter.
  replies: []
};
