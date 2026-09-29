<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import LogTail from '@bagel/ui/svelte/LogTail.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployItem, DeployStage } from '$lib/deploys/types';
  import ItemRow from './ItemRow.svelte';
  import NodeDots from './NodeDots.svelte';
  import { STAGE_KEY } from './view';

  let { stage }: { stage: DeployStage } = $props();

  const { t } = getI18n();

  const items = $derived(stage.items ?? []);
  const links = $derived(stage.links ?? []);
  const log = $derived(stage.failure?.log_tail ?? []);
</script>

{#snippet manifest(item: DeployItem)}
  <Tag tone={item.state === 'succeeded' ? 'live' : 'quiet'} mark={item.state === 'succeeded' ? 'solid' : 'hollow'}>
    {t('admin.deploys.manifest')}
  </Tag>
{/snippet}

<Stack gap={2}>
  {#if links.length > 0}
    <Cluster gap={3}>
      {#each links as l (l.url)}
        <TextLink href={l.url} label={l.label} external />
      {/each}
    </Cluster>
  {/if}

  {#if items.length > 0}
    <Scroller maxHeight="330px" role="region" aria-label={t(STAGE_KEY[stage.id])} tabindex={0}>
      <ul class="items">
        {#each items as item (item.key)}
          {#if stage.id === 'build'}
            <ItemRow {item}>{#snippet extra()}{@render manifest(item)}{/snippet}</ItemRow>
          {:else if stage.id === 'rollout'}
            <ItemRow {item}>{#snippet extra()}<NodeDots pods={item.nodes} />{/snippet}</ItemRow>
          {:else}
            <ItemRow {item} />
          {/if}
        {/each}
      </ul>
    </Scroller>
  {/if}

  {#if log.length > 0}
    <LogTail lines={log} label={t('admin.deploys.logLabel', { stage: t(STAGE_KEY[stage.id]) })} />
  {/if}
</Stack>

<style>
  .items {
    list-style: none;
    margin: 0;
    padding: 0;
  }
</style>
