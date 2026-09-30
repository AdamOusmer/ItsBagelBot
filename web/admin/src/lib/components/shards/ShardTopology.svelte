<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { IngressCapacity, ShardSnapshot, TrialSocket } from '@bagel/kit';
  import type { TrialChannel, TrialSnapshot } from '$lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { durationLabel } from '$lib/duration';
  import { RATE_NOW_SECONDS } from '@bagel/kit/rates';
  import type { StatusTone } from '@bagel/kit/status-tone';
  import LoadMeter from './LoadMeter.svelte';
  import { eventsPerSecond, utilizationPct } from '$lib/throughput';
  import ShardCard from './ShardCard.svelte';
  import { groupShardNodes } from './shard-topology';
  import { podIndex, rateLabel } from './shard-state';

  let { snapshot, capacity, trials, showTrials }: {
    snapshot: ShardSnapshot;
    capacity: IngressCapacity;
    trials: TrialSnapshot | null;
    showTrials: boolean;
  } = $props();
  const { t } = getI18n();
  const pods = $derived(groupShardNodes(snapshot.nodes, snapshot.shards));
  const conduit = $derived(snapshot.conduit_manager);
  const channels = $derived((trials?.trials ?? [])
    .filter((row) => row.state !== 'removed' && row.state !== 'promoted')
    .sort((a, b) => (a.display_name?.trim() || a.broadcaster_id).localeCompare(b.display_name?.trim() || b.broadcaster_id, undefined, { numeric: true })));
  type TrialGroup = { slot: number; socket?: TrialSocket; channels: TrialChannel[] };
  const trialGroups = $derived.by((): TrialGroup[] => {
    const sockets = snapshot.trial_sockets ?? [];
    const slots = new Set([...sockets.filter((socket) => socket.channels > 0).map((socket) => socket.slot), ...channels.map((trial) => trial.slot ?? 0)]);
    return [...slots].sort((a, b) => a - b).map((slot) => ({ slot, socket: sockets.find((socket) => socket.slot === slot), channels: channels.filter((trial) => (trial.slot ?? 0) === slot) }));
  });
  const SOCKET_TONE: Record<TrialSocket['state'], StatusTone> = { connected: 'success', connecting: 'warning', idle: 'neutral' };
  const SOCKET_LABEL: Record<TrialSocket['state'], string> = { connected: 'admin.shards.trialSocketConnected', connecting: 'admin.shards.trialSocketConnecting', idle: 'admin.shards.trialSocketIdle' };
  function burstRate(load?: number): number { return eventsPerSecond(load, capacity.burst_window_seconds ?? RATE_NOW_SECONDS); }
  const received = $derived(channels.reduce((total, trial) => total + (trial.received ?? 0), 0));
  const conduitReady = $derived(conduit?.state === 'ready' || conduit?.state === 'leader');
  function rate(load?: number): number { return eventsPerSecond(load, capacity.load_window_seconds); }
</script>

<section class="topology" aria-labelledby="topology-title">
  <header class="stack">
    <Heading level={5} as="h2" id="topology-title">{t('admin.shards.topologyTitle')}</Heading>
    <Text size="xs" tone="muted">{t('admin.shards.topologyBody')}</Text>
  </header>
  <div class="network">
    <Card as="section" aria-labelledby="production-title">
      <div class="network-body">
        <div class="network-head">
          <div class="stack"><Heading level={6} as="h3" id="production-title"><span class="path-mark" aria-hidden="true"></span>{t('admin.shards.production')}</Heading><Text size="xs" tone="muted">{t('admin.shards.productionHint')}</Text></div>
          <span class="network-count">{snapshot.shards.length}</span>
        </div>
        <div class="hub">
          <Card>
            <div class="hub-body">
              <div class="hub-icon" aria-hidden="true">◎</div>
              <div class="stack">
                <div class="line">
                  <strong class="hub-name">{t('admin.shards.conduitLabel')}</strong>
                  <span class="dotted"><StatusDot tone={conduitReady ? 'success' : 'warning'} /><Text as="span" size="xs" mono tone="muted">{conduit?.state ?? t('admin.shards.unknownState')}</Text></span>
                </div>
                <Text as="small" size="xs" mono tone="muted">{conduit?.node || '-'}{#if conduit?.conduit_id} · {conduit.conduit_id}{/if}</Text>
              </div>
            </div>
          </Card>
        </div>
        {#if pods.length}
          <ul class="branches pods" aria-label={t('admin.shards.listLabel')}>
            {#each pods as pod (pod.node)}
              <li class="branch pod">
                <Card>
                  <div class="stack pod-head">
                    <div class="line spread">
                      <strong class="hub-name">{pod.node ? t('admin.shards.podLabel', { pod: pod.pod }) : t('admin.shards.unassignedNode')}</strong>
                      <Text as="span" size="xs" mono tone="muted">{t('admin.shards.podSummary', { count: String(pod.shards.length), eps: rateLabel(pod.shards.reduce((total, shard) => total + rate(shard.load), 0)) })}</Text>
                    </div>
                    {#if pod.node}<Text as="small" size="xs" mono tone="muted" title={pod.node}>{pod.node}</Text>{/if}
                  </div>
                  {#if pod.shards.length}
                    <ul class="sockets">
                      {#each pod.shards as shard (shard.shard_id)}
                        <li><ShardCard {shard} pod={pod.pod} eps={rate(shard.load)} burstEps={burstRate(shard.burst_load)} utilization={utilizationPct(rate(shard.load), capacity.websocket_rated_eps)} ratedEps={capacity.websocket_rated_eps} targetUtilization={capacity.target_utilization_pct} /></li>
                      {/each}
                    </ul>
                  {:else}<div class="pod-empty"><Text size="xs" tone="muted">{t('admin.shards.empty')}</Text></div>{/if}
                </Card>
              </li>
            {/each}
          </ul>
        {:else}<EmptyState title={t('admin.shards.empty')} body={t('admin.shards.emptyBody')} />{/if}
      </div>
    </Card>
  </div>
  {#if showTrials}
    <div class="network trials">
      <Card as="section" dashed aria-labelledby="trials-title">
        <div class="network-body">
          <div class="network-head">
            <div class="stack"><Heading level={6} as="h3" id="trials-title"><span class="path-mark" aria-hidden="true"></span>{t('admin.shards.trialConnections')}</Heading><Text size="xs" tone="muted">{t('admin.shards.trialsHint')}</Text></div>
            <span class="network-count">{trials === null ? '-' : channels.length}</span>
          </div>
          <div class="hub">
            <Card dashed>
              <div class="hub-body">
                <div class="hub-icon" aria-hidden="true">◌</div>
                <div class="stack">
                  <strong class="hub-name">{t('admin.shards.trialReceiver')}</strong>
                  <Text as="small" size="xs" mono tone="muted">{trials === null ? t('admin.shards.trialUnavailable') : t('admin.shards.trialSummary', { count: String(channels.length), received: String(received) })}</Text>
                </div>
              </div>
            </Card>
          </div>
          {#if trials?.socket_target}<div class="after-hub"><Text size="xs" tone="muted">{t('admin.shards.trialConduit', { target: String(trials.socket_target), max: '3' })}</Text></div>{/if}
          {#if trials === null}<div class="after-hub"><Text size="xs" tone="muted">{t('admin.shards.trialUnavailable')}</Text></div>
          {:else if trialGroups.length === 0}<div class="after-hub"><Text size="xs" tone="muted">{t('admin.shards.trialEmpty')}</Text></div>
          {:else}
            <ul class="branches trial-channels">
              {#each trialGroups as group (group.slot)}
                <li class="branch trial-socket">
                  <Card dashed>
                    <div class="stack trial-socket-head">
                      <div class="line">
                        <strong class="trial-socket-name">{t('admin.shards.trialSocket', { id: String(group.slot) })}</strong>
                        <span class="dotted"><StatusDot tone={group.socket ? SOCKET_TONE[group.socket.state] : 'warning'} /><Text as="span" size="xs" tone="muted">{t(group.socket ? SOCKET_LABEL[group.socket.state] : 'admin.shards.trialSocketUnowned')}</Text></span>
                      </div>
                      <Text as="small" size="xs" mono tone="muted">{t(group.channels.length === 1 ? 'admin.shards.trialSocketMetaOne' : 'admin.shards.trialSocketMeta', { pod: (group.socket && podIndex(pods.map((pod) => pod.node), group.socket.node)) || '-', count: String(group.channels.length) })}</Text>
                      {#if group.socket}<Text as="small" size="xs" mono tone="muted">{group.socket.node}</Text>{/if}
                      <LoadMeter eps={rate(group.socket?.load)} burstEps={burstRate(group.socket?.burst)} utilization={utilizationPct(rate(group.socket?.load), capacity.websocket_rated_eps)} targetUtilization={capacity.target_utilization_pct} />
                    </div>
                    <ul class="sockets trial-members">
                      {#each group.channels as trial (trial.broadcaster_id)}
                        <li>
                          <Card as="article" dashed data-cursor="quiet">
                            <div class="trial-channel">
                              <header class="line spread">
                                <strong class="trial-channel-name">{trial.display_name?.trim() || trial.broadcaster_id}</strong>
                                <span class="dotted"><StatusDot tone={!trial.enabled ? 'neutral' : trial.state === 'receiving' ? 'success' : trial.state === 'failed' ? 'danger' : 'warning'} /><Text as="span" size="xs" tone="muted">{trial.enabled ? t(`admin.trials.state.${trial.state}`) : t('admin.trials.off')}</Text></span>
                              </header>
                              <Text as="small" size="xs" mono tone="muted">{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</Text>
                              <LoadMeter eps={rate(snapshot.trial_loads?.[trial.broadcaster_id])} burstEps={burstRate(snapshot.trial_burst_loads?.[trial.broadcaster_id])} utilization={utilizationPct(rate(snapshot.trial_loads?.[trial.broadcaster_id]), capacity.websocket_rated_eps)} targetUtilization={capacity.target_utilization_pct} />
                              <div class="trial-metrics">
                                <strong class="trial-received">{t('admin.trials.received', { count: String(trial.received ?? 0) })}</strong>
                                <Text as="span" size="xs" mono tone="muted">{t('admin.trials.decoded', { count: String(trial.decoded ?? 0) })}</Text>
                                <Text as="span" size="xs" mono tone="muted">{t('admin.trials.processed', { count: String(trial.processed ?? 0) })}</Text>
                                <Text as="span" size="xs" mono tone="muted">{t('admin.trials.failed', { count: String(trial.failed ?? 0) })}</Text>
                                <Text as="span" size="xs" mono tone="muted">{t('admin.trials.retried', { count: String(trial.retried ?? 0) })}</Text>
                                <Text as="span" size="xs" mono tone="muted">{t('admin.trials.blocked', { count: String(trial.blocked_actions ?? 0) })}</Text>
                                <Text as="span" size="xs" mono tone="muted">{t('admin.trials.latency', { duration: durationLabel(trial.average_processing_latency_ns ?? 0) })}</Text>
                              </div>
                              {#if trial.error}<Text size="xs" tone="danger">{trial.error}</Text>{/if}
                            </div>
                          </Card>
                        </li>
                      {/each}
                    </ul>
                  </Card>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      </Card>
    </div>
    <Text size="xs" mono tone="muted">{t('admin.shards.topologyLegend')}</Text>
  {/if}
</section>

<style>
  .topology { display: grid; gap: var(--bb-space-4); margin-top: var(--bb-space-5); overflow-wrap: anywhere; }
  .stack { display: grid; gap: var(--bb-space-1); min-width: 0; }
  .line { display: flex; align-items: center; flex-wrap: wrap; gap: var(--bb-space-2) var(--bb-space-3); }
  .spread { justify-content: space-between; }
  .dotted { display: inline-flex; align-items: center; gap: var(--bb-space-2); }
  .network { --path: var(--bb-green-glow); --wire: rgba(var(--bb-green-glow-rgb), .32); --line-style: solid; --card-pad: var(--bb-space-5); --card-bg: radial-gradient(ellipse at top left, rgba(var(--bb-green-glow-rgb), .045), transparent 65%); min-width: 0; }
  .network-body { --card-pad: var(--bb-space-4); --card-border: var(--wire); --card-bg: var(--bb-card-bg); }
  .network-head { display: flex; justify-content: space-between; align-items: start; gap: var(--bb-space-3); margin-bottom: var(--bb-space-5); }
  .path-mark { display: inline-block; vertical-align: middle; width: 18px; height: 0; margin-right: var(--bb-space-2); border-top: 2px var(--line-style) var(--path); }
  .network-count { flex: none; min-width: 30px; text-align: center; padding: var(--bb-space-1) var(--bb-space-2); border: 1px solid var(--wire); border-radius: var(--bb-radius-xs); color: var(--path); font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); }
  .hub { width: fit-content; max-width: 100%; margin-left: 18px; }
  .hub-body { display: flex; align-items: center; gap: var(--bb-space-3); }
  .hub-icon { color: var(--path); font-size: 29px; line-height: 1; }
  .hub-name { font-size: var(--bb-text-xs); font-weight: 600; }
  .after-hub { margin-top: var(--bb-space-4); }
  ul { list-style: none; padding: 0; margin: 0; }
  .branches { position: relative; display: grid; gap: var(--bb-space-4); margin-left: 39px; padding: var(--bb-space-5) 0 0 26px; }
  .branches::before { content: ''; position: absolute; top: 0; bottom: 0; left: 0; border-left: 1px var(--line-style) var(--wire); }
  .branch { position: relative; min-width: 0; }
  .branch::before { content: ''; position: absolute; left: -27px; top: 26px; width: 26px; border-top: 1px var(--line-style) var(--wire); }
  .branch:last-child::after { content: ''; position: absolute; top: 27px; bottom: -1px; left: -27px; width: 1px; background: var(--bb-card-bg); }
  .pod { --card-bg: rgba(var(--bb-green-glow-rgb), .025); }
  .pod-head, .trial-socket-head { margin-bottom: var(--bb-space-4); }
  .pod-empty { padding-top: var(--bb-space-4); }
  .sockets { --card-bg: var(--bb-card-bg); --card-border: var(--bb-border); display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 230px), 1fr)); gap: var(--bb-space-3); }
  .sockets > li { position: relative; min-width: 0; }
  .sockets > li::before { content: ''; position: absolute; top: -10px; left: 20px; height: 9px; border-left: 1px solid var(--wire); }
  .trials { --path: var(--bb-tan); --wire: rgba(var(--bb-tan-rgb), .35); --line-style: dashed; --card-bg: radial-gradient(ellipse at top left, rgba(var(--bb-tan-rgb), .05), transparent 65%); }
  .trial-socket { --card-bg: transparent; }
  .trial-members { --card-border: var(--wire); }
  .trial-socket-head { gap: var(--bb-space-2); }
  .trial-socket-name { font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); }
  .trial-channel { display: grid; gap: var(--bb-space-3); }
  .trial-channel-name { font-size: var(--bb-text-sm); }
  .trial-metrics { display: flex; flex-wrap: wrap; gap: var(--bb-space-2) var(--bb-space-4); }
  .trial-received { width: 100%; color: var(--path); font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); font-weight: 500; }
  @media (max-width: 600px) {
    .network { --card-pad: var(--bb-space-4); }
    .hub { --card-pad: var(--bb-space-3); margin-left: 0; }
    .branches { margin-left: 17px; padding-left: 17px; }
    .branch::before { left: -18px; width: 17px; }
    .branch:last-child::after { left: -18px; }
    .pod { --card-pad: var(--bb-space-3); }
  }
</style>
