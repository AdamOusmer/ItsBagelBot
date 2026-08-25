// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const TRIGGERS_MODULE: ModuleDef = 
{
  id: 'triggers',
  label: 'Trigger Words',
  tagline: 'Auto-reply when a word shows up in chat — no "!" needed.',
  description:
    'Give the bot a list of words or phrases and the line to post when it sees one in ordinary chat — no command prefix required. Each rule pairs a phrase with a response: pick how the phrase matches (whole word, contains, exact message or starts with — the default matches the whole word, so "hi" will not fire inside "this"), write the reply, and switch individual rules on or off without deleting them. Responses support {user}, {random} and {choice:a,b,c}. The first matching rule wins, so one message gets at most one reply.',
  icon: 'chat',
  category: 'Chat',
  defaultEnabled: false,
  // Trigger rules are a free-form list of phrase→response pairs the author grows,
  // so the module page renders them as add/removable ReplyRows with a bespoke
  // rule inspector (TriggerRuleEditor: phrase, match mode, response, per-rule
  // enabled switch). The whole list is persisted as one "rules" string holding
  // a JSON array of structured rules; the sesame module also still parses the
  // legacy "phrase => response" line format for configs saved before the
  // migration. See app/sesame/modules/triggers.go.
  replies: []
};
