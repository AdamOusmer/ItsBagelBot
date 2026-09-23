<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // One row of a stage breakdown: a PR being merged, an image being built or
  // checked, a service rolling. Fixed height whatever the state, so a row
  // gaining a detail or a failure never pushes the rows under it; the detail
  // is cut to one line and the full text rides on the title.
  //
  // `extra` is the stage's own column: the manifest mark for a build, the
  // per-node pod dots for a rollout.
  import type { Snippet } from 'svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployItem } from '$lib/deploys/types';
  import CheckDot from './CheckDot.svelte';
  import { STEP_STATE_KEY, ratio, stageTone } from './view';

  let { item, extra }: { item: DeployItem; extra?: Snippet } = $props();

  const { t } = getI18n();

  const value = $derived(item.state === 'succeeded' ? 1 : (ratio(item.progress) ?? null));
  const hasBar = $derived(item.progress.total > 0);
</script>

<li class="item">
  <CheckDot tone={stageTone(item.state)} label={t(STEP_STATE_KEY[item.state])} />
  {#if item.url}
    <a class="label" href={item.url} target="_blank" rel="noopener noreferrer">{item.label}</a>
  {:else}
    <span class="label">{item.label}</span>
  {/if}
  <span class="bar">
    {#if hasBar}<ProgressBar {value} tone={stageTone(item.state)} label={item.label} size="sm" />{/if}
  </span>
  <span class="extra">{#if extra}{@render extra()}{/if}</span>
  <span class="detail" title={item.detail}>{item.detail ?? ''}</span>
</li>

<style>
  .item {
    display: grid;
    grid-template-columns: 12px minmax(120px, 200px) 120px minmax(0, auto) minmax(0, 1fr);
    align-items: center;
    gap: 12px;
    height: 32px;
    border-bottom: 1px solid var(--rule);
  }
  .item:last-child {
    border-bottom: none;
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-white);
  }
  a.label {
    text-decoration: underline;
    text-decoration-color: var(--rule);
    text-underline-offset: 3px;
  }
  .extra {
    display: inline-flex;
    min-width: 0;
  }
  .detail {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    color: var(--bb-muted);
  }
  @media (max-width: 760px) {
    .item {
      grid-template-columns: 12px minmax(0, 1fr) 80px;
    }
    .extra,
    .detail {
      display: none;
    }
  }
</style>
