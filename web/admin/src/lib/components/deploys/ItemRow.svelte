<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployItem } from '#lib/deploys/types';
  import CheckDot from './CheckDot.svelte';
  import { STEP_STATE_KEY, ratio, stageTone } from './view';

  let { item, extra }: { item: DeployItem; extra?: Snippet } = $props();

  const { t } = getI18n();

  const value = $derived(item.state === 'succeeded' ? 1 : (ratio(item.progress) ?? null));
  const hasBar = $derived(item.progress.total > 0);
</script>

<li class="item">
  <CheckDot tone={stageTone(item.state)} label={t(STEP_STATE_KEY[item.state])} />
  <span class="label">
    <Text as="span" size="xs" mono>
      {#if item.url}<TextLink variant="inline" href={item.url} external>{item.label}</TextLink>{:else}{item.label}{/if}
    </Text>
  </span>
  <span class="bar">
    {#if hasBar}<ProgressBar {value} tone={stageTone(item.state)} label={item.label} size="sm" />{/if}
  </span>
  <span class="extra">{#if extra}{@render extra()}{/if}</span>
  {#if item.detail}
    <span class="detail"><Text as="span" size="xs" tone="muted">{item.detail}</Text></span>
  {/if}
</li>

<style>
  .item {
    display: grid;
    grid-template-columns: 12px minmax(0, 1.1fr) minmax(0, .7fr) minmax(0, auto) minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-3);
    min-height: 40px;
  }
  .extra {
    display: inline-flex;
    min-width: 0;
  }
  .label,
  .detail {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .detail {
    grid-column: 5;
  }
  .extra:empty {
    display: none;
  }
  @container (max-width: 36rem) {
    .item {
      grid-template-columns: 12px minmax(0, 1fr) minmax(0, 4rem);
      column-gap: var(--bb-space-3);
      row-gap: var(--bb-space-1);
      padding-block: var(--bb-space-2);
    }
    .extra,
    .detail {
      grid-column: 2 / -1;
    }
  }
</style>
