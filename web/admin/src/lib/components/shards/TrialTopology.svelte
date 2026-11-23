<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { IngressCapacity, ShardSnapshot, TrialSocket } from '@bagel/kit';
  import type { TrialSnapshot } from '#lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RATE_AVG_SECONDS, RATE_NOW_SECONDS } from '@bagel/kit/rates';
  import type { StatusTone } from '@bagel/kit/status-tone';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { eventsPerSecond, utilizationPct } from '#lib/throughput';
  import LoadMeter from './LoadMeter.svelte';
  import TrialChannelCard from './TrialChannelCard.svelte';
  import { groupTrialConnections, type TrialSocketGroup } from './shard-topology';
  import { podIndex } from './shard-state';

  let { snapshot, capacity, trials, nodes, loading, stale }: {
    snapshot: ShardSnapshot | null;
    capacity: IngressCapacity | null;
    trials: TrialSnapshot | null;
    nodes: string[];
    loading: boolean;
    stale: boolean;
  } = $props();

  const { locale, t } = getI18n();
  const sockets = {
    connected: { tone: 'success', label: 'admin.shards.trialSocketConnected' },
    connecting: { tone: 'warning', label: 'admin.shards.trialSocketConnecting' },
    idle: { tone: 'neutral', label: 'admin.shards.trialSocketIdle' }
  } as const satisfies Record<TrialSocket['state'], { tone: StatusTone; label: string }>;
  const topology = $derived(groupTrialConnections(snapshot?.trial_sockets ?? [], trials?.trials ?? [], locale));
  const loadingGroups: TrialSocketGroup[] = [{ slot: 0, channels: [] }];
  const groups = $derived(loading && topology.groups.length === 0 ? loadingGroups : topology.groups);
  const target = $derived(trials?.socket_target ?? 0);
  const statusCopy = $derived(trials ? topology.count.toLocaleString(locale) : '–');
  const receiver = $derived(trials
    ? t('admin.shards.trialSummary', { count: topology.count.toLocaleString(locale), received: topology.received.toLocaleString(locale) })
    : t(loading ? 'admin.shards.trialLoading' : 'admin.shards.throughputUnavailable'));
</script>

<Card as="section" dashed aria-label={t('admin.shards.trialConnections')} aria-busy={loading} style="--card-border: var(--bb-rule)">
  <CardHead title={t('admin.shards.trialConnections')} headingLevel={3}>
    {#snippet actions()}
      <div class="trial-status" role="status">
        <Tag tone={stale && trials ? 'warning' : 'neutral'} contain>
          <bb-fit><span data-fit>{trials && stale ? t('admin.shards.stale') : statusCopy}</span></bb-fit>
        </Tag>
      </div>
    {/snippet}
  </CardHead>
  <Stack gap={4}>
    <div class="receiver">
      <Heading level={6} as="div">{t('admin.shards.trialReceiver')}</Heading>
      <Text size="xs" tone="muted"><bb-fit><span data-fit>{receiver}</span></bb-fit></Text>
    </div>

    <div class="conduit-summary" class:visible={loading || target > 0} aria-hidden={!loading && target === 0}>
      {#if loading}
        <div aria-hidden="true"><Skeleton lines={2} /></div>
      {:else}
        <Text size="sm" tone="muted" style="block-size: 100%">
          <bb-fit mode="block">
            <span data-fit>{t('admin.shards.trialConduit', { target, max: 3 })}</span>
          </bb-fit>
        </Text>
      {/if}
    </div>

    {#if !loading && trials === null}
      <AlertBanner role="status">{t('admin.shards.trialUnavailable')}</AlertBanner>
    {:else if !groups.length}
      <EmptyState title={t('admin.shards.trialEmpty')} />
    {:else}
      <ul class="trial-groups" role="list" aria-label={t('admin.shards.trialConnections')}>
        {#each groups as group (group.slot)}
          {@const title = t('admin.shards.trialSocket', { id: group.slot })}
          {@const status = group.socket ? sockets[group.socket.state] : undefined}
          {@const fallback = snapshot ? 'admin.shards.trialSocketUnowned' : 'admin.shards.unknownState'}
          {@const count = loading ? group.socket?.channels ?? 1 : group.channels.length}
          {@const pod = podIndex(nodes, group.socket?.node) || '–'}
          {@const eps = eventsPerSecond(group.socket?.load, capacity?.load_window_seconds ?? RATE_AVG_SECONDS)}
          <li class="trial-group">
            <div class="socket-head">
              {#if loading && !group.socket}
                <div aria-hidden="true"><Skeleton lines={3} /></div>
              {:else}
                <Stack gap={1}>
                    <div class="socket-title">
                      <Heading level={6} as="h4"><bb-fit><span data-fit>{title}</span></bb-fit></Heading>
                      <div class="socket-status">
                        <StatusDot tone={status?.tone ?? 'warning'} flat />
                        <Text size="xs" tone="muted">
                          <bb-fit><span data-fit>{t(status?.label ?? fallback)}</span></bb-fit>
                        </Text>
                      </div>
                    </div>
                    <Text size="xs" mono tone="muted">
                      <bb-fit>
                        <span data-fit>{t(count === 1 ? 'admin.shards.trialSocketMetaOne' : 'admin.shards.trialSocketMeta', { pod, count })}</span>
                      </bb-fit>
                    </Text>
                    <Text size="xs" mono tone="muted" title={group.socket?.node}>
                      <bb-fit><span data-fit>{group.socket?.node || '–'}</span></bb-fit>
                    </Text>
                </Stack>
              {/if}
              <div class="socket-load">
                {#if loading && !group.socket}
                  <div aria-hidden="true"><Skeleton lines={2} /></div>
                {:else if !capacity || !group.socket}
                  <Text size="xs" tone="muted">
                    <bb-fit><span data-fit>{t('admin.shards.throughputUnavailable')}</span></bb-fit>
                  </Text>
                {:else}
                  <LoadMeter
                    {eps}
                    burstEps={eventsPerSecond(group.socket?.burst, capacity.burst_window_seconds ?? RATE_NOW_SECONDS)}
                    utilization={utilizationPct(eps, capacity.websocket_rated_eps)}
                    targetUtilization={capacity.target_utilization_pct}
                  />
                {/if}
              </div>
            </div>
            {#if loading}
              <div class="channel-list" aria-hidden="true">
                {#each Array(count) as _, index (index)}
                  <Skeleton variant="block" height="var(--trial-channel-height)" />
                {/each}
              </div>
            {:else if group.channels.length}
              <ul class="channel-list" role="list" aria-label={title}>
                {#each group.channels as trial (trial.broadcaster_id)}
                  <li class="channel">
                    <TrialChannelCard {trial} {snapshot} {capacity} />
                  </li>
                {/each}
              </ul>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </Stack>
</Card>

<style>
  .trial-status {
    display: grid;
    inline-size: 6.5rem;
  }

  .receiver {
    display: grid;
    gap: var(--bb-space-1);
    min-inline-size: 0;
    padding: var(--bb-space-3) var(--bb-space-4);
    border-radius: var(--bb-radius-md);
    background: var(--bb-elev-2);
  }

  .conduit-summary {
    block-size: 2.7rem;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .conduit-summary.visible {
    opacity: 1;
  }

  .trial-groups {
    container-type: inline-size;
    display: grid;
    gap: var(--bb-space-4);
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .trial-group {
    min-inline-size: 0;
    padding-block: var(--bb-space-3) 0;
    border-block-start: 1px dashed var(--bb-rule);
  }

  .socket-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 18rem);
    align-items: center;
    gap: var(--bb-space-3);
  }

  .socket-title {
    display: grid;
    grid-template-columns: minmax(0, 1fr) min(47%, 10rem);
    align-items: baseline;
    gap: var(--bb-space-2);
  }

  .socket-status {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-2);
  }

  .socket-load {
    min-block-size: 2.75rem;
  }

  .channel-list {
    --trial-channel-height: calc(13rem + 2 * var(--card-pad));
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 17rem), 1fr));
    gap: var(--bb-space-3);
    list-style: none;
    margin: 0;
    padding: var(--bb-space-3) 0 0;
  }

  .channel {
    container-type: inline-size;
    min-inline-size: 0;
  }

  @media (max-width: 650px) {
    .conduit-summary {
      block-size: 3.75rem;
    }
  }

  @container (max-width: 35rem) {
    .socket-head {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--bb-space-2);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .conduit-summary { transition: none; }
  }
</style>
