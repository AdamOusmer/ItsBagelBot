// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleField } from './module-def';

export const LINKED_ONLY_FIELD: ModuleField = {
  key: 'linkedOnly',
  label: 'Only look up my linked account',
  type: 'toggle',
  help: 'When on, viewers cannot name another player: every command shows your linked account (or your Twitch username if none is linked) and any typed name is ignored without a chat reply. Off by default.'
};

export const MINECRAFT_UUID_FIELD: ModuleField = {
  key: 'accountUuid',
  label: 'Minecraft UUID',
  type: 'text',
  hidden: true
};
