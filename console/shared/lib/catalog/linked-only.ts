// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleField } from './module-def';

// LINKED_ONLY_FIELD is the "only look up my linked account" toggle every
// lookup module (Fortnite, Valorant, Clash Royale, Bedwars, MCSR) appends to
// its settings. One definition so the key, label and semantics cannot drift
// between modules: sesame reads the same "linkedOnly" key in each config
// struct via lookupLinkedOnly (app/twitch/sesame/modules/external.go). Off by
// default, so an unset key keeps the "viewers can name any player" behaviour.
export const LINKED_ONLY_FIELD: ModuleField = {
  key: 'linkedOnly',
  label: 'Only look up my linked account',
  type: 'toggle',
  help: 'When on, viewers cannot name another player: every command shows your linked account (or your Twitch username if none is linked) and any typed name is ignored without a chat reply. Off by default.'
};
