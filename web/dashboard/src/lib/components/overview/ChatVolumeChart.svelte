<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Chat volume across the stream: one point per minute, plus tan ticks on the
  // minutes a command answered.
  //
  // Only the wording is the dashboard's: the drawing lives in the shared
  // AreaSeries so a second console charting the same lane cannot draw it
  // differently. What stays here is what a shared chart must not decide -- the
  // label, the now/peak readout, the legend, and what to say when the read did
  // not land.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import type { ChatVolume } from '$lib/overview-live';

  const { t } = getI18n();

  let { volume }: { volume: ChatVolume } = $props();

  // Two points are the minimum a curve can be drawn from; below that (and on a
  // failed read) the panel says so rather than drawing an empty box.
  const hasCurve = $derived(volume.ok && volume.buckets.length > 1);
</script>

<div class="ov-vol">
  <div class="ov-vol__head">
    <span class="ov-vol__label">{t('overview.chatVolume')}</span>
    {#if volume.ok}
      <span class="ov-vol__rate"
        >{t('overview.chatVolumeNowPeak', { now: volume.now, peak: volume.peak })}</span
      >
    {/if}
  </div>

  {#if hasCurve}
    <AreaSeries
      values={volume.buckets}
      ticks={volume.commandTicks}
      ariaLabel={t('overview.chatVolumeChartLabel')}
    />
    <p class="ov-vol__legend">
      <span class="ov-vol__swatch" aria-hidden="true"></span>
      {t('overview.chatVolumeLegend')}
    </p>
  {:else}
    <p class="ov-vol__empty">{t('overview.chatVolumeUnavailable')}</p>
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
  .ov-vol__label,
  .ov-vol__rate {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-vol__legend {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 12px 0 0;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-vol__swatch {
    width: 10px;
    height: 2px;
    background: var(--bb-tan);
    display: inline-block;
    flex: none;
  }
  .ov-vol__empty {
    margin: 18px 0 0;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
  }
</style>
