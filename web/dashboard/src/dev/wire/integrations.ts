// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SUB } from '#lib/server/services/subjects';
import { spotifyScopes } from '#lib/server/oauth';
import { demoGoveeDevices } from '../fixtures';
import { demoRuns } from '../flow-fixtures';
import type { Payload, Routes } from './types';

const keys = { govee: true, spotify: true, spotifyApp: true };
let spotifyClientId = 'demo-client-id';
let rewardCount = 0;

function setKey(name: keyof typeof keys, present: boolean) {
  return () => {
    keys[name] = present;
    return { ok: true };
  };
}

function saveSpotifyApp(payload: Payload) {
  keys.spotifyApp = true;
  spotifyClientId = String(payload.client_id ?? '');
  return { ok: true };
}

function rewardReply(payload: Payload) {
  const reward = (payload.reward ?? {}) as Record<string, unknown>;
  const id = String(payload.reward_id ?? reward.id ?? `demo-reward-${++rewardCount}`);
  return { reward: { ...reward, id } };
}

function goveeScenes(payload: Payload) {
  if (payload.device !== demoGoveeDevices()[0].device) return { scenes: [] };
  return { scenes: [{ id: 'lightScene:101', name: 'Sunset glow', type: 'devices.capabilities.dynamic_scene', instance: 'lightScene', value: 101 }] };
}

export const integrationRoutes: Routes = {
  [`${SUB.goveeKey}.status`]: () => ({ present: keys.govee }),
  [`${SUB.goveeKey}.set`]: setKey('govee', true),
  [`${SUB.goveeKey}.clear`]: setKey('govee', false),
  [`${SUB.gossip}.govee.devices`]: () => (keys.govee ? { devices: demoGoveeDevices() } : { key_rejected: true }),
  [`${SUB.gossip}.govee.scenes`]: goveeScenes,
  [`${SUB.spotifyKey}.status`]: () => ({ present: keys.spotify, scopes: spotifyScopes(), needs_reconnect: false }),
  [`${SUB.spotifyKey}.clear`]: setKey('spotify', false),
  [`${SUB.spotifyKey}.app.status`]: () => ({ present: keys.spotifyApp, client_id: keys.spotifyApp ? spotifyClientId : '' }),
  [`${SUB.spotifyKey}.app.set`]: saveSpotifyApp,
  [`${SUB.spotifyKey}.app.clear`]: setKey('spotifyApp', false),
  [`${SUB.gossip}.spotify.playerqueue`]: () => ({ error: 'unavailable' }),
  [`${SUB.outgressRpc}.channelpoints.create`]: rewardReply,
  [`${SUB.outgressRpc}.channelpoints.update`]: rewardReply,
  [`${SUB.outgressRpc}.channelpoints.list`]: () => ({ rewards: [] }),
  [`${SUB.automations}.runs`]: (payload) => ({ runs: demoRuns(payload.automation_id as string | undefined) }),
  [`${SUB.automations}.run`]: () => ({ accepted: true, run_id: `demo-run-${Date.now()}` })
};
