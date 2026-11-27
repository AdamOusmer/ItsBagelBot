// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MOD } from '@bagel/kit';
import { markEdge, pageOutcome, readVisibility, type EdgeOutcome } from '#lib/server/edge-cache';
import { readModuleBlob } from '#lib/server/module-blob';
import { requireChannel, hideChannel } from '#lib/server/queue-channel';
import { readPublicSongQueue } from '#lib/server/songqueue-public-store';
import { userSongQueuePage } from '#lib/server/services';
import { songSnapshotOf, type SongQueueSnapshot } from '#lib/songqueue-snapshot';

type SrBlob = { sr?: { enabled?: boolean } };

export type SongQueueRead = { login: string; channelName: string; snapshot: SongQueueSnapshot; outcome: EdgeOutcome };

export async function readSongQueueChannel(login: string, locals: App.Locals): Promise<SongQueueRead> {
  const channel = await requireChannel(login, locals);
  const blob = readModuleBlob<SrBlob>(channel.userId, MOD.songqueue);
  const [enabled, pageOn, view, open] = await Promise.all([
    readVisibility(blob.then((b) => b.enabled)),
    readVisibility(userSongQueuePage(channel.userId)),
    readPublicSongQueue(channel.userId),
    blob.then((b) => b.configs.sr?.enabled !== false).catch(() => false)
  ]);
  if (enabled === false || pageOn === false) hideChannel(locals);
  return {
    login: channel.login,
    channelName: channel.channelName,
    snapshot: songSnapshotOf({ ...view, open }, Date.now()),
    outcome: pageOutcome([enabled, pageOn], view.degraded)
  };
}
