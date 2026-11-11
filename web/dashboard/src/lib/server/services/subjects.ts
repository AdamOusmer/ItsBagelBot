// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// process.env, not $app/env/private: it only exposes variables declared in src/env.ts.
// `||`, not `??`: a set-but-blank env var would build subjects no responder answers.
export const SUB = {
  broadcaster: process.env.NATS_BROADCASTER_STATUS_SUBJECT || 'rpc.broadcaster.status.get',
  dashboard: process.env.NATS_DASHBOARD_SUBJECT_PREFIX || 'rpc.dashboard',
  commands: process.env.NATS_COMMANDS_SUBJECT_PREFIX || 'rpc.commands',
  modules: process.env.NATS_MODULES_SUBJECT_PREFIX || 'rpc.modules',
  automations: process.env.NATS_AUTOMATIONS_SUBJECT_PREFIX || 'rpc.automations',
  projector: process.env.NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX || 'rpc.projector.dashboard',
  outgress: process.env.NATS_OUTGRESS_SYSTEM_SUBJECT || 'twitch.outgress.system',
  outgressRpc: process.env.NATS_OUTGRESS_RPC_PREFIX || 'rpc.outgress',
  gossip: process.env.NATS_GOSSIP_SUBJECT_PREFIX || 'rpc.gossip',
  loyalty: process.env.NATS_LOYALTY_SUBJECT_PREFIX || 'rpc.loyalty',
  goveeKey: process.env.NATS_MODULES_GOVEE_SUBJECT_PREFIX || 'rpc.modules.govee',
  spotifyKey: process.env.NATS_MODULES_SPOTIFY_SUBJECT_PREFIX || 'rpc.modules.spotify',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX || 'rpc.admin.user.audit',
  delegation: process.env.NATS_DELEGATION_SUBJECT_PREFIX || 'rpc.delegation',
  notifications: process.env.NATS_NOTIFICATIONS_SUBJECT_PREFIX || 'rpc.notifications',
  transactions: process.env.NATS_TRANSACTIONS_SUBJECT_PREFIX || 'rpc.transactions'
};
