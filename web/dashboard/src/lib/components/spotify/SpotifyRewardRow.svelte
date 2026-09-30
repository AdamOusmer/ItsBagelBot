<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Icon, ManagementRow, Tag, Text } from '@bagel/ui/svelte';
  import { getI18n, type SpotifyRedeemConfig } from '@bagel/kit';
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
          <span class="light-name" class:unset={!bound}>{t('spotify.reward.rowLabel')}</span>
          <Text as="span" size="xs" mono tone="muted">{t('spotify.reward.rowHint')}</Text>
        </span>
      </span>
      <span class="status">
        {#if bound && reward}
          <Text as="span" size="sm" truncate>{reward.title || t('spotify.reward.thisReward')}</Text>
          <Text as="span" size="xs" mono tone="accent">{t('spotify.reward.costPts', { n: reward.cost.toLocaleString() })}</Text>
        {:else}
          <span class="unset-tag"><Tag tone="quiet" mark="hollow">{t('spotify.reward.notSetUp')}</Tag></span>
        {/if}
      </span>
      <span class="chev" class:open={expanded} aria-hidden="true"><Icon name="chevron" size={13} /></span>
    </span>
  {/snippet}
  {#snippet actions()}
    {#if bound}
      <RowDeleteButton label={t('spotify.reward.removeAria', { name: reward?.title || t('spotify.reward.thisReward') })} onclick={onDelete} />
    {/if}
  {/snippet}
</ManagementRow>

<style>
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
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .light-name.unset { color: var(--bb-muted); }

  .status { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .unset-tag { align-self: flex-start; }

  .chev {
    display: inline-flex;
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast) ease, transform var(--bb-dur-fast) ease;
  }
  .chev.open { color: var(--bb-tan); transform: rotate(180deg); }

  @media (max-width: 620px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'light chev' 'status chev'; row-gap: 4px; }
    .light { grid-area: light; }
    .status { grid-area: status; }
    .chev { grid-area: chev; }
  }
</style>
