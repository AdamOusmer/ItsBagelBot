<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { ManagementRow, Switch, Tag } from '@bagel/ui/svelte';
  import { getI18n, type ChannelPointReward } from '@bagel/kit';
  import RowDeleteButton from '$lib/components/shared/RowDeleteButton.svelte';

  const { t } = getI18n();

  let {
    reward,
    index = undefined as number | undefined,
    expanded = false,
    onExpand,
    onDelete,
    toggleSubmit
  }: {
    reward: ChannelPointReward;
    index?: number;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
    toggleSubmit: SubmitFunction;
  } = $props();

  const r = $derived(reward);
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const togglePayload = $derived(JSON.stringify({ ...r, isEnabled: !r.isEnabled }));
</script>

<ManagementRow as="li" selected={expanded} {expanded} controls="reward-editor" onSelect={onExpand}>
  {#snippet primary()}
    <span class="prow">
      {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
      <span class="reward">
        <span class="reward-name">
          <span class="swatch" style="--sw: {r.backgroundColor || '#9147ff'}" aria-hidden="true"></span>
          <span class="title-text">{r.title}</span>
        </span>
        <span class="tags">
          {#if r.maxPerStreamEnabled && r.maxPerStream === 1}
            <Tag tone="bare">{t('channelpoints.chipOnce')}</Tag>
          {:else if r.maxPerStreamEnabled}
            <Tag tone="bare">{t('channelpoints.chipPerStream', { n: r.maxPerStream })}</Tag>
          {/if}
          {#if r.maxPerUserPerStreamEnabled}<Tag tone="bare">{t('channelpoints.chipPerUser', { n: r.maxPerUserPerStream })}</Tag>{/if}
          {#if r.globalCooldownEnabled}<Tag tone="bare">{t('channelpoints.chipCooldown', { n: r.globalCooldownSeconds })}</Tag>{/if}
          {#if r.isUserInputRequired}<Tag tone="bare">{t('channelpoints.chipInput')}</Tag>{/if}
          {#if r.onRedeem === 'cancel'}<Tag tone="bare">{t('channelpoints.chipRefund')}</Tag>{/if}
          {#if r.counter}<Tag tone="bare">{t('channelpoints.chipCounterName', { name: r.counter })}</Tag>{/if}
          {#if r.points > 0}<Tag tone="bare">{t('channelpoints.chipPointsAward', { n: r.points })}</Tag>{/if}
        </span>
      </span>
      <span class="resp">
        {#if r.action === 'chat'}
          {r.message || '{user} redeemed {reward}!'}
        {:else}
          <span class="silent">{t('channelpoints.chipSilent')}</span>
        {/if}
      </span>
      <span class="meta">
        <span class="cost"><span class="bb-sr-only">{t('channelpoints.fieldCost')}: </span>{r.cost.toLocaleString()}</span>
        <Tag tone={r.isEnabled ? 'live' : 'quiet'} mark={r.isEnabled ? 'solid' : 'hollow'}>
          {r.isEnabled ? t('channelpoints.stateVisible') : t('channelpoints.stateHidden')}
        </Tag>
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <form method="POST" action="?/update" use:enhance={toggleSubmit}>
      <input type="hidden" name="reward" value={togglePayload} />
      <Switch type="submit" checked={r.isEnabled} label={t('channelpoints.toggleAria', { name: r.title })} />
    </form>
    <RowDeleteButton label={t('channelpoints.deleteAria', { name: r.title })} onclick={onDelete} />
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(150px, 1fr) minmax(0, 1.6fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); color: var(--bb-muted); opacity: 0.55; }

  .reward { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .reward-name {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
    min-width: 0;
  }
  .title-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: var(--bb-radius-sm);
    background: var(--sw);
    border: 1px solid color-mix(in srgb, var(--sw) 55%, transparent);
  }

  .tags { display: flex; flex-wrap: wrap; gap: 4px 14px; }
  .resp {
    font-family: var(--bb-font-body);
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .silent { opacity: 0.6; font-style: italic; }

  .meta { display: inline-flex; flex-direction: column; align-items: flex-end; gap: 4px; }
  .cost {
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  @media (max-width: 760px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'reward'
        'resp'
        'meta';
      row-gap: 6px;
    }
    .idx { display: none; }
    .reward { grid-area: reward; }
    .resp {
      grid-area: resp;
      white-space: normal;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      -webkit-box-orient: vertical;
    }
    .meta { grid-area: meta; flex-direction: row; align-items: center; gap: 12px; }
  }
</style>
