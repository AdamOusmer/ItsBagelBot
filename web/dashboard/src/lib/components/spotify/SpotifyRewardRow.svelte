<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import '@bagel/ui/styles/elements/row-body.css';
  import '@bagel/ui/styles/elements/swatch.css';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type SpotifyRedeemConfig } from '@bagel/kit';
  import RowDeleteButton from '#lib/components/shared/RowDeleteButton.svelte';

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
  onSelect={onExpand}
>
  {#snippet primary()}
    <span class="bb-row-body bb-row-body--device">
      <span class="bb-row-body__lead">
        <span class="bb-swatch" style="--sw: {reward?.color || '#1db954'}" aria-hidden="true"></span>
        <span class="bb-row-body__stack">
          <span class="bb-row-body__name bb-row-body__name--display" class:bb-row-body__name--muted={!bound}>{t('spotify.reward.rowLabel')}</span>
          <Text as="span" size="xs" mono tone="muted">{t('spotify.reward.rowHint')}</Text>
        </span>
      </span>
      <span class="bb-row-body__stack bb-row-body__status">
        {#if bound && reward}
          <Text as="span" size="sm" truncate>{reward.title || t('spotify.reward.thisReward')}</Text>
          <Text as="span" size="xs" mono tone="accent">{t('spotify.reward.costPts', { n: reward.cost.toLocaleString() })}</Text>
        {:else}
          <span class="unset-tag"><Tag tone="quiet" mark="hollow">{t('spotify.reward.notSetUp')}</Tag></span>
        {/if}
      </span>
      <span class="bb-row-body__chev" class:is-open={expanded} aria-hidden="true"><Icon name="chevron" size={13} /></span>
    </span>
  {/snippet}
  {#snippet actions()}
    {#if bound}
      <RowDeleteButton label={t('spotify.reward.removeAria', { name: reward?.title || t('spotify.reward.thisReward') })} onclick={onDelete} />
    {/if}
  {/snippet}
</ManagementRow>

<style>
  .unset-tag { align-self: flex-start; }
</style>
