<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { IngressCapacity, ShardSnapshot, TrialSocket } from '@bagel/kit';
  import type { TrialChannel, TrialSnapshot } from '$lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
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
  <header class="topology-head">
    <h2 class="topology-title" id="topology-title">{t('admin.shards.topologyTitle')}</h2>
    <p class="topology-description">{t('admin.shards.topologyBody')}</p>
  </header>
  <section class="network production" aria-labelledby="production-title">
    <div class="network-head">
      <div><h3 class="network-title" id="production-title"><span class="path-mark" aria-hidden="true"></span>{t('admin.shards.production')}</h3><p class="network-description">{t('admin.shards.productionHint')}</p></div>
      <span class="network-count">{snapshot.shards.length}</span>
    </div>
    <div class="hub">
      <div class="hub-icon" aria-hidden="true">◎</div>
      <div class="hub-content">
        <strong>{t('admin.shards.conduitLabel')}</strong>
        <span class="hub-state"><StatusDot tone={conduitReady ? 'success' : 'warning'} />{conduit?.state ?? t('admin.shards.unknownState')}</span>
        <small class="hub-meta">{conduit?.node || '—'}{#if conduit?.conduit_id} · {conduit.conduit_id}{/if}</small>
      </div>
    </div>
    {#if pods.length}
      <ul class="branches pods" aria-label={t('admin.shards.listLabel')}>
        {#each pods as pod (pod.node)}
          <li class="branch pod">
            <div class="pod-head">
              <strong>{pod.node ? t('admin.shards.podLabel', { pod: pod.pod }) : t('admin.shards.unassignedNode')}</strong>
              <span class="pod-summary">{t('admin.shards.podSummary', { count: String(pod.shards.length), eps: rateLabel(pod.shards.reduce((total, shard) => total + rate(shard.load), 0)) })}</span>
              {#if pod.node}<small class="pod-node" title={pod.node}>{pod.node}</small>{/if}
            </div>
            {#if pod.shards.length}
              <ul class="sockets">
                {#each pod.shards as shard (shard.shard_id)}
                  <li><ShardCard {shard} pod={pod.pod} eps={rate(shard.load)} burstEps={burstRate(shard.burst_load)} utilization={utilizationPct(rate(shard.load), capacity.websocket_rated_eps)} ratedEps={capacity.websocket_rated_eps} targetUtilization={capacity.target_utilization_pct} /></li>
                {/each}
              </ul>
            {:else}<p class="empty">{t('admin.shards.empty')}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}<EmptyState title={t('admin.shards.empty')} body={t('admin.shards.emptyBody')} />{/if}
  </section>
  {#if showTrials}
    <section class="network trials" aria-labelledby="trials-title">
      <div class="network-head">
        <div><h3 class="network-title" id="trials-title"><span class="path-mark" aria-hidden="true"></span>{t('admin.shards.trialConnections')}</h3><p class="network-description">{t('admin.shards.trialsHint')}</p></div>
        <span class="network-count">{trials === null ? '—' : channels.length}</span>
      </div>
      <div class="hub">
        <div class="hub-icon" aria-hidden="true">◌</div>
        <div class="hub-content">
          <strong>{t('admin.shards.trialReceiver')}</strong>
          <small class="hub-meta">{trials === null ? t('admin.shards.trialUnavailable') : t('admin.shards.trialSummary', { count: String(channels.length), received: String(received) })}</small>
        </div>
      </div>
      {#if trials?.socket_target}<p class="trial-conduit">{t('admin.shards.trialConduit', { target: String(trials.socket_target), max: '3' })}</p>{/if}
      {#if trials === null}<p class="empty">{t('admin.shards.trialUnavailable')}</p>
      {:else if trialGroups.length === 0}<p class="empty">{t('admin.shards.trialEmpty')}</p>
      {:else}
        <ul class="branches trial-channels">
          {#each trialGroups as group (group.slot)}
            <li class="branch trial-socket">
              <div class="trial-socket-head">
                <strong>{t('admin.shards.trialSocket', { id: String(group.slot) })}</strong>
                <span class="trial-state"><StatusDot tone={group.socket ? SOCKET_TONE[group.socket.state] : 'warning'} />{t(group.socket ? SOCKET_LABEL[group.socket.state] : 'admin.shards.trialSocketUnowned')}</span>
                <small class="trial-socket-meta">{t(group.channels.length === 1 ? 'admin.shards.trialSocketMetaOne' : 'admin.shards.trialSocketMeta', { pod: (group.socket && podIndex(pods.map((pod) => pod.node), group.socket.node)) || '-', count: String(group.channels.length) })}</small>
                {#if group.socket}<small class="trial-socket-meta">{group.socket.node}</small>{/if}
                <div class="socket-load"><LoadMeter eps={rate(group.socket?.load)} burstEps={burstRate(group.socket?.burst)} utilization={utilizationPct(rate(group.socket?.load), capacity.websocket_rated_eps)} targetUtilization={capacity.target_utilization_pct} /></div>
              </div>
              <ul class="sockets trial-members">
              {#each group.channels as trial (trial.broadcaster_id)}
                <li>
              <article class="trial-channel" data-cursor="quiet">
                <header>
                  <strong>{trial.display_name?.trim() || trial.broadcaster_id}</strong>
                  <span class="trial-state"><StatusDot tone={!trial.enabled ? 'neutral' : trial.state === 'receiving' ? 'success' : trial.state === 'failed' ? 'error' : 'warning'} />{trial.enabled ? t(`admin.trials.state.${trial.state}`) : t('admin.trials.off')}</span>
                </header>
                <small class="trial-channel-id">{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</small>
                <div class="socket-load"><LoadMeter eps={rate(snapshot.trial_loads?.[trial.broadcaster_id])} burstEps={burstRate(snapshot.trial_burst_loads?.[trial.broadcaster_id])} utilization={utilizationPct(rate(snapshot.trial_loads?.[trial.broadcaster_id]), capacity.websocket_rated_eps)} targetUtilization={capacity.target_utilization_pct} /></div>
                <div class="trial-metrics">
                  <strong>{t('admin.trials.received', { count: String(trial.received ?? 0) })}</strong>
                  <span>{t('admin.trials.decoded', { count: String(trial.decoded ?? 0) })}</span>
                  <span>{t('admin.trials.processed', { count: String(trial.processed ?? 0) })}</span>
                  <span>{t('admin.trials.failed', { count: String(trial.failed ?? 0) })}</span>
                  <span>{t('admin.trials.retried', { count: String(trial.retried ?? 0) })}</span>
                  <span>{t('admin.trials.blocked', { count: String(trial.blocked_actions ?? 0) })}</span>
                  <span>{t('admin.trials.latency', { duration: durationLabel(trial.average_processing_latency_ns ?? 0) })}</span>
                </div>
                {#if trial.error}<p class="trial-error">{trial.error}</p>{/if}
              </article>
                </li>
              {/each}
              </ul>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
    <p class="legend">{t('admin.shards.topologyLegend')}</p>
  {/if}
</section>

<style>
  .topology { margin-top: 24px; }
  .topology-head { margin-bottom: 18px; }
  h2, h3, p { margin: 0; }
  .topology-title { font-size: 17px; font-weight: 600; color: var(--bb-white); }
  .topology-description, .network-description { margin-top: 5px; color: var(--bb-muted); font-size: 12px; line-height: 1.6; }
  .network { --path: var(--bb-green-glow); --wire: rgba(var(--bb-green-glow-rgb), .32); --line-style: solid; padding: 22px; border: 1px solid var(--bb-border); border-radius: 14px; background: radial-gradient(ellipse at top left, rgba(var(--bb-green-glow-rgb), .045), transparent 65%); }
  .network + .network { margin-top: 18px; }
  .network-head { display: flex; justify-content: space-between; align-items: start; gap: 12px; margin-bottom: 22px; }
  .network-title { display: flex; align-items: center; gap: 9px; font-family: var(--bb-font-mono); font-size: 13px; font-weight: 600; }
  .path-mark { display: inline-block; width: 18px; height: 0; border-top: 2px var(--line-style) var(--path); }
  .network-count { flex: none; min-width: 30px; text-align: center; padding: 5px 9px; border: 1px solid var(--wire); border-radius: 6px; color: var(--path); font-family: var(--bb-font-mono); font-size: 12px; }
  .hub { display: flex; align-items: center; gap: 12px; width: fit-content; max-width: 100%; margin-left: 18px; padding: 14px 20px; border: 1px var(--line-style) var(--wire); border-radius: 10px; background: var(--bb-card-bg); }
  .hub-icon { color: var(--path); font-size: 29px; line-height: 1; }
  .hub-content { display: flex; flex-wrap: wrap; align-items: center; gap: 7px 14px; min-width: 0; }
  .hub-content > strong { font-size: 12px; font-weight: 600; }
  .hub-state { display: inline-flex; align-items: center; gap: 6px; font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); }
  .hub-meta { width: 100%; overflow-wrap: anywhere; font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); }
  ul { list-style: none; padding: 0; margin: 0; }
  .branches { position: relative; display: grid; gap: 18px; margin-left: 39px; padding: 24px 0 0 26px; }
  .branches::before { content: ''; position: absolute; top: 0; bottom: 0; left: 0; border-left: 1px var(--line-style) var(--wire); }
  .branch { position: relative; min-width: 0; }
  .branch::before { content: ''; position: absolute; left: -27px; top: 26px; width: 26px; border-top: 1px var(--line-style) var(--wire); }
  .branch:last-child::after { content: ''; position: absolute; top: 27px; bottom: -1px; left: -27px; width: 1px; background: var(--bb-card-bg); }
  .pod { padding: 16px; border: 1px solid var(--wire); border-radius: 10px; background: rgba(var(--bb-green-glow-rgb), .025); }
  .pod-head { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 10px; margin-bottom: 17px; }
  .pod-head > strong { font-size: 12px; }
  .pod-summary { margin-left: auto; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; }
  .pod-node { width: 100%; overflow-wrap: anywhere; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; }
  .sockets { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 230px), 1fr)); gap: 12px; }
  .sockets > li { position: relative; min-width: 0; }
  .sockets > li::before { content: ''; position: absolute; top: -10px; left: 20px; height: 9px; border-left: 1px solid var(--wire); }
  .empty { padding: 15px 0 0; color: var(--bb-muted); font-size: 12px; }
  .trials { --path: var(--bb-tan); --wire: rgba(var(--bb-tan-rgb), .35); --line-style: dashed; background: radial-gradient(ellipse at top left, rgba(var(--bb-tan-rgb), .05), transparent 65%); border-style: dashed; }
  .trial-conduit { margin-top: 16px; color: var(--bb-muted); font-size: 12px; line-height: 1.6; }
  .trial-socket { padding: 16px; border: 1px dashed var(--wire); border-radius: 10px; }
  .trial-socket-head { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; margin-bottom: 18px; }
  .trial-socket-head > strong { font-family: var(--bb-font-mono); font-size: 12px; }
  .trial-socket-meta { width: 100%; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; overflow-wrap: anywhere; }
  .socket-load { width: 100%; margin-top: 12px; }
  .socket-load :global(.load) { width: 100%; flex-wrap: wrap; gap: 7px; }
  .socket-load :global(.bar) { flex-basis: 100%; }
  .socket-load :global(.rate) { display: block; font-size: 10px; }
  .trial-channel { padding: 15px; border: 1px dashed var(--wire); border-radius: 10px; background: var(--bb-card-bg); }
  .trial-channel header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
  .trial-channel strong { font-size: 13px; overflow-wrap: anywhere; }
  .trial-state { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-muted); font-size: 10px; }
  .trial-channel-id { display: block; margin-top: 7px; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; }
  .trial-metrics { display: flex; flex-wrap: wrap; gap: 8px 18px; margin-top: 16px; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; }
  .trial-metrics > strong { width: 100%; color: var(--path); font-size: 12px; font-weight: 500; }
  .trial-channel > .trial-error { margin-top: 12px; color: var(--bb-status-error); font-size: 10px; overflow-wrap: anywhere; }
  .legend { margin-top: 13px; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; }
  @media (max-width: 600px) {
    .network { padding: 15px; }
    .hub { margin-left: 0; padding: 12px; }
    .branches { margin-left: 17px; padding-left: 17px; }
    .branch::before { left: -18px; width: 17px; }
    .branch:last-child::after { left: -18px; }
    .pod { padding: 12px; }
  }
</style>
