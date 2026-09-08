// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleField } from './module-def';

// Settings fields more than one module's def shares, verbatim. Named
// shared-fields rather than linked-only since the linked-account toggle stopped
// being the only one: the hidden UUID mirror below arrived with it.

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

// MINECRAFT_UUID_FIELD is the hidden mirror of the linked Minecraft account.
// The dashboard resolves the account name to a UUID on save and stores it here
// (see dashboard/src/lib/server/minecraft-uuid.ts); sesame prefers the UUID so
// a rename does not break a binding. Hidden because it is derived, not typed:
// showing it would invite editing the one field a broadcaster cannot know.
// Byte-identical in the MCSR and Bedwars/urchin defs before this, which is
// exactly the drift risk: sesame reads one key name from both.
export const MINECRAFT_UUID_FIELD: ModuleField = {
  key: 'accountUuid',
  label: 'Minecraft UUID',
  type: 'text',
  hidden: true
};
