// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The module catalog is one file per module under catalog/: adding a module
// means adding one file plus one line in MODULE_CATALOG below, mirroring how
// app/sesame/modules/all.go registers the Go side of the same module. The
// loyalty-wager games stay split in ../catalog-games and valorant in
// ../catalog-valorant (same pattern, older cut) and are spread in here.
//
// MOD maps every module id used in the catalog. Module-id strings were
// previously hardcoded per store — a typo compiled fine and silently missed
// the module blob — so stores key off MOD.<name> instead of a raw literal.
import type { ModuleDef } from './module-def';
import { GAME_MODULE_DEFS } from '../catalog-games';
import { VALORANT_MODULE_DEF } from '../catalog-valorant';
import { ALERTS_MODULE } from './alerts';
import { AUTOMOD_MODULE } from './automod';
import { CHANNELPOINTS_MODULE } from './channelpoints';
import { CLASHROYALE_MODULE } from './clashroyale';
import { COUNTERS_MODULE } from './counters';
import { EMOTEPLAY_MODULE } from './emoteplay';
import { FORTNITE_MODULE } from './fortnite';
import { GOVEE_MODULE } from './govee';
import { LOYALTY_MODULE } from './loyalty';
import { MCSR_MODULE } from './mcsr';
import { QUEUE_MODULE } from './queue';
import { QUOTES_MODULE } from './quotes';
import { RAFFLE_MODULE } from './raffle';
import { SHOUTOUT_MODULE } from './shoutout';
import { SONGQUEUE_MODULE } from './songqueue';
import { TIMERS_MODULE } from './timers';
import { TIME_MODULE } from './time';
import { TRIGGERS_MODULE } from './triggers';
import { URCHIN_MODULE } from './urchin';

export const MOD = {
  channelpoints: 'channelpoints',
  timers: 'timers',
  loyalty: 'loyalty',
  counters: 'counters',
  gamble: 'gamble',
  duel: 'duel',
  triggers: 'triggers',
  time: 'time',
  emoteplay: 'emoteplay',
  automod: 'automod',
  shoutout: 'shoutout',
  alerts: 'alerts',
  urchin: 'urchin',
  mcsr: 'mcsr',
  fortnite: 'fortnite',
  clashroyale: 'clashroyale',
  valorant: 'valorant',
  queue: 'queue',
  raffle: 'raffle',
  quotes: 'quotes',
  govee: 'govee',
  songqueue: 'songqueue'
} as const;

export const MODULE_CATALOG: readonly ModuleDef[] = [
  // Order here is declaration order, not the directory grouping. The modules
  // index sorts by MODULE_CATEGORY_ORDER (Moderation → Chat → Channel →
  // Points → Play → Gear → Stats) so AutoMod is the first row a streamer
  // sees. Channel Points and Timers own bespoke pages (opened via href);
  // Trigger Words uses the generic reply inspector with its rule editor.
  CHANNELPOINTS_MODULE,
  TIMERS_MODULE,
  LOYALTY_MODULE,
  COUNTERS_MODULE,
  ...GAME_MODULE_DEFS,
  TRIGGERS_MODULE,
  TIME_MODULE,
  EMOTEPLAY_MODULE,
  AUTOMOD_MODULE,
  SHOUTOUT_MODULE,
  ALERTS_MODULE,
  URCHIN_MODULE,
  MCSR_MODULE,
  FORTNITE_MODULE,
  CLASHROYALE_MODULE,
  VALORANT_MODULE_DEF,
  QUEUE_MODULE,
  RAFFLE_MODULE,
  QUOTES_MODULE,
  GOVEE_MODULE,
  SONGQUEUE_MODULE
];

export function moduleDef(id: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((m) => m.id === id);
}

// catalogChildren are the modules nested under parentId, in catalog order.
export function catalogChildren(parentId: string): ModuleDef[] {
  return MODULE_CATALOG.filter((def) => def.parent === parentId);
}

// catalogIndexable is the modules grid's row set: hidden modules stay
// unreachable, nested children fold into their parent instead of minting a
// second tile that could be flipped on without it.
export function catalogIndexable(def: ModuleDef): boolean {
  return !def.hidden && !def.parent;
}
