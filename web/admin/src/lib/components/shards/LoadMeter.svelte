<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { pctLabel } from '#lib/throughput';
  import { loadTone, rateLabel } from './shard-state';

  let {
    eps,
    burstEps,
    utilization,
    targetUtilization,
    showTarget = false
  }: {
    eps: number;
    burstEps?: number;
    utilization: number;
    targetUtilization: number;
    showTarget?: boolean;
  } = $props();

  const { t } = getI18n();

  const RATE_TONE = { success: 'success', warning: 'accent', danger: 'danger', neutral: 'muted' } as const;

  const tone = $derived(loadTone(utilization, targetUtilization));
  const load = $derived({ eps: rateLabel(eps), pct: pctLabel(utilization) });
  const rate = $derived(
    burstEps === undefined
      ? t('admin.shards.rowLoad', load)
      : t('admin.shards.rowLoadBurst', { ...load, now: rateLabel(burstEps) })
  );
</script>

<Stack gap={2}>
  <Text as="div" size="xs" tone={RATE_TONE[tone]} title={rate} style="block-size: 2lh; font-variant-numeric: tabular-nums">
    <bb-fit mode="block"><span data-fit>{rate}</span></bb-fit>
  </Text>
  <ProgressBar
    value={utilization / 100}
    {tone}
    target={showTarget ? targetUtilization / 100 : undefined}
    label={rate}
    size="sm"
    aria-hidden="true"
  />
</Stack>
