<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
	import '@bagel/ui/styles/elements/feed.css';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ServiceHealth } from '$lib/server/services';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';

  let { probes, ok }: { probes: ServiceHealth[]; ok: boolean } = $props();

  const { t } = getI18n();

  const responding = $derived(probes.filter((p) => p.ok).length);
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
    <Text as="span" size="sm">
      {t('admin.overview.healthSummary', {
        ok: String(responding),
        total: String(probes.length)
      })}
    </Text>
  </p>

  {#if probes.length}
    <Scroller maxHeight="220px">
      <div class="bb-node-list bb-stagger">
        {#each probes as p (p.id)}
          <div class="bb-node-list__row">
            <StatusDot tone={statusTone(p.ok ? 'online' : 'degraded')} />
            <span class="bb-node-list__name">{p.label}</span>
            <span class="bb-node-list__trail">
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
    gap: var(--bb-space-2);
    margin: 0 0 10px;
  }
</style>
