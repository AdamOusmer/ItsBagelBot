<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
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

  const tone = $derived(loadTone(utilization, targetUtilization));
  const width = $derived(Math.min(100, Math.max(0, Math.round(utilization))));
</script>

<span class="load">
  <span class="bar" aria-hidden="true">
    <span class="fill {tone}" style="width:{width}%"></span>
  </span>
  <span class="rate {tone}">
    {burstEps === undefined
      ? t('admin.shards.rowLoad', { eps: rateLabel(eps), pct: pctLabel(utilization) })
      : t('admin.shards.rowLoadBurst', { now: rateLabel(burstEps), eps: rateLabel(eps), pct: pctLabel(utilization) })}
  </span>
</span>

<style>
  .load {
    display: flex;
    align-items: center;
    gap: 9px;
    flex: none;
    width: 200px;
  }
  .bar {
    flex: 1;
    height: 6px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.08);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: var(--bb-radius-pill);
    transition: width 0.3s ease;
  }
  .fill.success {
    background: var(--bb-green-glow);
  }
  .fill.warning {
    background: var(--bb-tan);
  }
  .fill.error {
    background: var(--bb-status-error);
  }
  .fill.neutral {
    background: rgba(255, 255, 255, 0.18);
  }
  .rate {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    white-space: nowrap;
  }
  .rate.success {
    color: var(--bb-green-glow);
  }
  .rate.warning {
    color: var(--bb-tan-light);
  }
  .rate.error {
    color: var(--bb-status-error);
  }
  .rate.neutral {
    color: var(--bb-muted);
  }

  @media (max-width: 760px) {
    .load {
      width: 110px;
    }
    .rate {
      display: none;
    }
  }
</style>
