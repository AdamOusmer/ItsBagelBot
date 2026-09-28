<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Icon, ManagementRow, getI18n, type SpotifyRedeemConfig } from '@bagel/kit';
  import RowDeleteButton from '$lib/components/shared/RowDeleteButton.svelte';

  const { t } = getI18n();

  let {
    redeem,
    expanded = false,
    onExpand,
    onDelete
  }: {
    redeem: SpotifyRedeemConfig;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
  } = $props();

  const reward = $derived(redeem.reward ?? null);
  const bound = $derived(!!redeem.rewardId);
</script>

<div class="row-wrap" class:unset={!bound}>
  <ManagementRow
    selected={expanded}
    {expanded}
    controls="spotify-editor"
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
        <span class="light">
          <span class="swatch" style="--sw: {reward?.color || '#1db954'}" aria-hidden="true"></span>
          <span class="light-text">
            <span class="light-name">{t('spotify.rewardRowLabel')}</span>
            <span class="light-sku">{t('spotify.rewardRowHint')}</span>
          </span>
        </span>
        <span class="status">
          {#if bound && reward}
            <span class="reward-title">{reward.title || t('spotify.thisReward')}</span>
            <span class="reward-cost">{t('spotify.costPts', { n: reward.cost.toLocaleString() })}</span>
          {:else}
            <span class="unset-tag bb-tag bb-tag--quiet"><i class="bb-mark bb-mark--hollow" aria-hidden="true"></i>{t('spotify.notSetUp')}</span>
          {/if}
        </span>
        <span class="chev" class:open={expanded} aria-hidden="true"><Icon name="chevron" size={13} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      {#if bound}
        <RowDeleteButton label={t('spotify.removeAria', { name: reward?.title || t('spotify.thisReward') })} onclick={onDelete} />
      {/if}
    {/snippet}
  </ManagementRow>
</div>

<style>
  .row-wrap.unset .light-name { color: var(--bb-muted); }

  .prow {
    display: grid;
    grid-template-columns: minmax(150px, 1.2fr) minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }

  .light { display: inline-flex; align-items: center; gap: 10px; min-width: 0; }
  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: none;
    border-radius: var(--bb-radius-sm);
    background: var(--sw);
    border: 1px solid color-mix(in srgb, var(--sw) 55%, transparent);
  }
  .light-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .light-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .light-sku { font-family: var(--bb-font-mono, monospace); font-size: 11px; color: var(--bb-muted); }

  .status { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .reward-title {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .reward-cost { font-family: var(--bb-font-mono, monospace); font-size: 11.5px; color: var(--bb-tan-light); }
  .unset-tag { align-self: flex-start; }

  .chev {
    display: inline-flex;
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast, 140ms) ease, transform var(--bb-dur-fast, 140ms) ease;
  }
  .chev.open { color: var(--bb-tan); transform: rotate(180deg); }

  @media (max-width: 620px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'light chev' 'status chev'; row-gap: 4px; }
    .light { grid-area: light; }
    .status { grid-area: status; }
    .chev { grid-area: chev; }
  }
</style>
