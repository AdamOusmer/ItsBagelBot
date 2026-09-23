<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // What sits under a stage row: its links, its per-item breakdown, and the
  // log tail when it failed.
  //
  // The breakdown scrolls inside a bounded list rather than growing the page.
  // A release lists every image three times (build, digests, pin) and every
  // service once more for the rollout; unbounded, the stage being watched
  // ends up a screen below the one that just finished.
  //
  // The build row shows one bar for all of an image's jobs (both
  // architectures and the manifest) plus a manifest mark: the build stage
  // reports one item per image with the job count as its progress, so a
  // per-architecture split is not in the data this page receives.
  import LogTail from '@bagel/ui/svelte/LogTail.svelte';
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

{#if links.length > 0}
  <p class="links">
    {#each links as l (l.url)}
      <TextLink href={l.url} label={l.label} external size="11.5px" />
    {/each}
  </p>
{/if}

{#if items.length > 0}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <!-- The same exception LogTail and Table document: a bounded box that
       SCROLLS has to take keyboard focus (WCAG 2.1.1). Rollout rows hold no
       link to tab through, and Safari never makes a scroller focusable, so
       without this the services past the tenth row are pointer-only. The
       region wraps the list rather than replacing its role, so the rows
       still read as a list. -->
  <div class="items" role="region" aria-label={t(STAGE_KEY[stage.id])} tabindex="0">
  <ul>
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
  </div>
{/if}

{#if log.length > 0}
  <LogTail lines={log} label={t('admin.deploys.logLabel', { stage: t(STAGE_KEY[stage.id]) })} />
{/if}

<style>
  .links {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
    margin: 0 0 6px;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
  }
  .items {
    margin: 0 0 8px;
    max-height: 330px;
    overflow-y: auto;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
</style>
