<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The /health probe, summarised. One row per responder with its round-trip
  // time, because "which one" and "how slow" are the two follow-up questions an
  // operator asks the instant the count is not N/N.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ServiceHealth } from '$lib/server/services';
  import StatusDot from '../StatusDot.svelte';

  let { probes, ok }: { probes: ServiceHealth[]; ok: boolean } = $props();

  const { t } = getI18n();

  const responding = $derived(probes.filter((p) => p.ok).length);
  // A failed read of the probe list is 'unavailable' (neutral), not 'degraded':
  // we did not learn that the services are down, we learned nothing.
  const tone = $derived(
    !ok
      ? statusTone('unavailable')
      : statusTone(responding === probes.length ? 'online' : 'degraded')
  );
</script>

<Card as="section">
  <CardHead title={t('admin.overview.healthTitle')} />

  <p class="summary">
    <StatusDot {tone} />
    <span>
      {t('admin.overview.healthSummary', {
        ok: String(responding),
        total: String(probes.length)
      })}
    </span>
  </p>

  {#if probes.length}
    <Scroller maxHeight="220px" data-lenis-prevent>
      <div class="node-list">
        {#each probes as p (p.id)}
          <div class="node-row">
            <StatusDot tone={statusTone(p.ok ? 'online' : 'degraded')} />
            <span class="nm">{p.label}</span>
            <span class="pg">
              {p.ok
                ? t('admin.overview.healthMs', { ms: String(p.ms) })
                : t('admin.overview.healthDown')}
            </span>
          </div>
        {/each}
      </div>
    </Scroller>
  {:else}
    <EmptyState title={t('admin.overview.healthEmpty')} />
  {/if}
</Card>

<style>
  .summary {
    display: flex;
    align-items: center;
    gap: 9px;
    margin: 0 0 10px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
  }
</style>
