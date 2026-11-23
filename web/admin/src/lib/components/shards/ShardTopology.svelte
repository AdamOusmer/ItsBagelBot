<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { IngressCapacity, ShardSnapshot } from '@bagel/kit';
  import type { TrialSnapshot } from '#lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RATE_AVG_SECONDS, RATE_NOW_SECONDS } from '@bagel/kit/rates';
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
  import ShardCard from './ShardCard.svelte';
  import TrialTopology from './TrialTopology.svelte';
  import { groupShardNodes } from './shard-topology';
  import { conduitStatus, rateLabel } from './shard-state';

  let {
    snapshot,
    capacity,
    trials,
    showProduction,
    showTrials,
    fleetLoading = false,
    trialsLoading = false,
    trialsStale = false
  }: {
    snapshot: ShardSnapshot | null;
    capacity: IngressCapacity | null;
    trials: TrialSnapshot | null;
    showProduction: boolean;
    showTrials: boolean;
    fleetLoading?: boolean;
    trialsLoading?: boolean;
    trialsStale?: boolean;
  } = $props();

  const { locale, t } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);
  const pods = $derived(groupShardNodes(snapshot?.nodes ?? [], snapshot?.shards ?? []));
  const nodes = $derived(pods.map((pod) => pod.node));
  const conduit = $derived(snapshot?.conduit_manager);
  const manager = $derived(conduitStatus(conduit?.state));
  const conduitAddress = $derived([conduit?.node, conduit?.conduit_id].filter(Boolean).join(' · '));
  const inventoryUnavailable = $derived(snapshot?.inventory === 'unavailable');
  const shardCount = $derived(snapshot?.shards.length ?? 0);

  function rate(load?: number): number {
    return eventsPerSecond(load, capacity?.load_window_seconds ?? RATE_AVG_SECONDS);
  }

  function burstRate(load?: number): number {
    return eventsPerSecond(load, capacity?.burst_window_seconds ?? RATE_NOW_SECONDS);
  }
</script>

<Stack as="section" gap={4} aria-labelledby="topology-title">
  <Stack as="header" gap={1}>
    <Heading level={5} as="h2" id="topology-title">{t('admin.shards.topologyTitle')}</Heading>
    <div class="topology-description">
      <Text size="sm" tone="muted" style="block-size: 100%">
        <bb-fit mode="block"><span data-fit>{t('admin.shards.topologyBody')}</span></bb-fit>
      </Text>
    </div>
  </Stack>

  {#if showProduction}
    <Card as="section" aria-label={t('admin.shards.production')} aria-busy={fleetLoading}>
      <CardHead title={t('admin.shards.production')} headingLevel={3}>
        {#snippet actions()}
          <Tag tone="neutral">{snapshot && !inventoryUnavailable ? numberFormat.format(shardCount) : '–'}</Tag>
        {/snippet}
      </CardHead>
      <Stack gap={4}>
        <div class="conduit">
          <div class="conduit-head">
            <Heading level={6} as="div">
              <bb-fit><span data-fit>{t('admin.shards.conduitLabel')}</span></bb-fit>
            </Heading>
            <div class="conduit-health">
              <StatusDot tone={manager.tone} flat />
              <Text size="xs" tone="muted" title={t(manager.label)}>
                <bb-fit><span data-fit>{t(manager.label)}</span></bb-fit>
              </Text>
            </div>
          </div>
          <Text size="xs" mono tone="muted" title={conduitAddress}>
            <bb-fit><span data-fit>{conduitAddress || '–'}</span></bb-fit>
          </Text>
        </div>

        {#if fleetLoading}
          <div class="pod-list" aria-hidden="true">
            {#each [0, 1] as index (index)}
              <div class="pod">
                <div class="pod-head">
                  <Skeleton width="50%" />
                  <Skeleton width="7rem" />
                </div>
                <div class="pod-address"><Skeleton width="10rem" /></div>
                <div class="socket-list">
                  {#each [0, 1] as slot (slot)}
                    <Skeleton variant="block" height="var(--socket-height)" />
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        {:else if inventoryUnavailable}
          <EmptyState title={t('admin.shards.inventoryUnavailable')} body={t('admin.shards.inventoryEmpty')} />
        {:else if capacity && shardCount > 0}
          <ul class="pod-list" role="list" aria-label={t('admin.shards.listLabel')}>
            {#each pods as pod (pod.node)}
              {@const title = pod.node ? t('admin.shards.podLabel', { pod: pod.pod }) : t('admin.shards.unassignedNode')}
              {@const eps = pod.shards.reduce((total, shard) => total + rate(shard.load), 0)}
              <li class="pod">
                <div class="pod-head">
                  <Heading level={6} as="h4"><bb-fit><span data-fit>{title}</span></bb-fit></Heading>
                  <div class="pod-total">
                    <Text size="xs" mono tone="muted">
                      <bb-fit><span data-fit>{t('admin.shards.podSummary', { count: numberFormat.format(pod.shards.length), eps: rateLabel(eps) })}</span></bb-fit>
                    </Text>
                  </div>
                </div>
                <div class="pod-address">
                  <Text size="xs" mono tone="muted" title={pod.node}>
                    <bb-fit><span data-fit>{pod.node || '–'}</span></bb-fit>
                  </Text>
                </div>
                {#if pod.shards.length}
                  <ul class="socket-list" role="list" aria-label={title}>
                    {#each pod.shards as shard (shard.shard_id)}
                      {@const shardRate = rate(shard.load)}
                      <li class="socket">
                        <ShardCard
                          {shard}
                          pod={pod.pod}
                          eps={shardRate}
                          burstEps={burstRate(shard.burst_load)}
                          utilization={utilizationPct(shardRate, capacity.websocket_rated_eps)}
                          ratedEps={capacity.websocket_rated_eps}
                          targetUtilization={capacity.target_utilization_pct}
                        />
                      </li>
                    {/each}
                  </ul>
                {:else}
                  <EmptyState title={t('admin.shards.empty')} />
                {/if}
              </li>
            {/each}
          </ul>
        {:else}
          <EmptyState
            title={t(snapshot ? 'admin.shards.empty' : 'admin.shards.throughputUnavailable')}
            body={snapshot ? t('admin.shards.emptyBody') : undefined}
          />
        {/if}
      </Stack>
    </Card>
  {/if}

  {#if showTrials}
    <TrialTopology {snapshot} {capacity} {trials} {nodes} loading={trialsLoading} stale={trialsStale} />
  {/if}
</Stack>

<style>
  .topology-description {
    block-size: 1.5rem;
  }

  .conduit {
    display: grid;
    gap: var(--bb-space-1);
    min-inline-size: 0;
    padding: var(--bb-space-3) var(--bb-space-4);
    border-radius: var(--bb-radius-md);
    background: var(--bb-elev-2);
  }

  .conduit-head, .pod-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) min(47%, 13rem);
    align-items: baseline;
    gap: var(--bb-space-2);
  }

  .conduit-health {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-2);
  }

  .pod-total {
    text-align: end;
  }

  .pod-list {
    display: grid;
    gap: var(--bb-space-4);
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .pod {
    min-inline-size: 0;
    padding-block: var(--bb-space-3) 0;
    border-block-start: 1px solid var(--bb-rule);
  }

  .pod-head {
    min-block-size: 1.275rem;
  }

  .pod-address {
    min-block-size: 1.1rem;
  }

  .socket-list {
    --socket-height: calc(9rem + 2 * var(--card-pad) + 3 * var(--bb-space-2));
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 15.5rem), 1fr));
    gap: var(--bb-space-3);
    list-style: none;
    margin: 0;
    padding: var(--bb-space-3) 0 0;
  }

  .socket {
    min-inline-size: 0;
  }

  @media (max-width: 650px) {
    .topology-description {
      block-size: 2.7rem;
    }
  }
</style>
