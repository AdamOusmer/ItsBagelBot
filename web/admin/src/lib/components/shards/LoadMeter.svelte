<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { pctLabel } from '$lib/throughput';
  import { loadTone, rateLabel } from './shard-state';

  let {
    eps,
    burstEps,
    utilization,
    targetUtilization
  }: {
    eps: number;
    burstEps?: number;
    utilization: number;
    targetUtilization: number;
  } = $props();

  const { t } = getI18n();

  const RATE_TONE = { success: 'success', warning: 'accent', danger: 'danger', neutral: 'muted' } as const;

  const tone = $derived(loadTone(utilization, targetUtilization));
  const rate = $derived(
    burstEps === undefined
      ? t('admin.shards.rowLoad', { eps: rateLabel(eps), pct: pctLabel(utilization) })
      : t('admin.shards.rowLoadBurst', { now: rateLabel(burstEps), eps: rateLabel(eps), pct: pctLabel(utilization) })
  );
</script>

<div class="load">
  <ProgressBar value={utilization / 100} {tone} label={rate} aria-hidden="true" />
  <Text as="span" size="xs" mono tone={RATE_TONE[tone]}>{rate}</Text>
</div>

<style>
  .load {
    display: grid;
    gap: var(--bb-space-2);
    width: 100%;
    overflow-wrap: anywhere;
  }
</style>
