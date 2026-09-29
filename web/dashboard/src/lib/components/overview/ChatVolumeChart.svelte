<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { ChatVolume } from '$lib/overview-live';

  const { t } = getI18n();

  let { volume }: { volume: ChatVolume } = $props();

  const hasCurve = $derived(volume.ok && volume.buckets.length > 1);
</script>

<div class="ov-vol">
  <div class="ov-vol__head">
    <Label mono as="span">{t('overview.chatVolume')}</Label>
    {#if volume.ok}
      <Label mono as="span">{t('overview.chatVolumeNowPeak', { now: volume.now, peak: volume.peak })}</Label>
    {/if}
  </div>

  {#if hasCurve}
    <AreaSeries
      values={volume.buckets}
      ticks={volume.commandTicks}
      ariaLabel={t('overview.chatVolumeChartLabel')}
    />
    <div class="ov-vol__legend">
      <span class="ov-vol__swatch" aria-hidden="true"></span>
      <Label mono as="span">{t('overview.chatVolumeLegend')}</Label>
    </div>
  {:else}
    <div class="ov-vol__empty"><Text size="sm" tone="muted">{t('overview.chatVolumeUnavailable')}</Text></div>
  {/if}
</div>

<style>
  .ov-vol {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .ov-vol__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 6px;
  }
  .ov-vol__legend {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }
  .ov-vol__swatch {
    width: 10px;
    height: 2px;
    background: var(--bb-tan);
    display: inline-block;
    flex: none;
  }
  .ov-vol__empty {
    margin-top: 18px;
  }
</style>
