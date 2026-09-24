<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import type { StatusTone } from '@bagel/kit/status-tone';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import LoadMeter from './LoadMeter.svelte';

  let {
    tone,
    name,
    state,
    meta,
    eps,
    burstEps,
    utilization,
    targetUtilization,
    marks
  }: {
    tone: StatusTone;
    name: string;
    state: string;
    meta: string;
    eps: number;
    burstEps?: number;
    utilization: number;
    targetUtilization: number;
    marks?: Snippet;
  } = $props();
</script>

<div class="row" data-cursor="quiet">
  <StatusDot {tone} />
  <span class="who">
    <span class="name">
      {name}
      <span class="state">{state}</span>
    </span>
    <span class="meta">{meta}</span>
  </span>
  <LoadMeter {eps} {burstEps} {utilization} {targetUtilization} />
  {#if marks}
    <span class="marks">{@render marks()}</span>
  {/if}
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    padding: 12px 14px;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .state {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .marks {
    display: flex;
    gap: 6px;
    flex: none;
  }
</style>
