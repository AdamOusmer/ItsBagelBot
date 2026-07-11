<script lang="ts">
  // One ledger line in the reward deck, on the shared ManagementRow: the
  // clickable primary is a real button and the quick actions (show/hide switch,
  // delete) are siblings of it. The page passes the enhance handlers so all
  // optimistic-UI state lives in one place.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, ManagementRow, Switch, getI18n, type ChannelPointReward } from '@bagel/shared';

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

<div class="row-wrap">
  <ManagementRow
    selected={expanded}
    {expanded}
    disabled={!r.isEnabled}
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
        {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
        <span class="reward">
          <span class="reward-name">
            <span class="swatch" style="--sw: {r.backgroundColor || '#9147ff'}"><Icon name="gem" size={11} /></span>
            {r.title}
            {#if !r.isEnabled}<span class="hidden-tag">{t('channelpoints.hiddenTag')}</span>{/if}
          </span>
          <span class="tags">
            {#if r.maxPerStreamEnabled && r.maxPerStream === 1}
              <span class="tag">{t('channelpoints.chipOnce')}</span>
            {:else if r.maxPerStreamEnabled}
              <span class="tag">{t('channelpoints.chipPerStream', { n: r.maxPerStream })}</span>
            {/if}
            {#if r.maxPerUserPerStreamEnabled}<span class="tag">{t('channelpoints.chipPerUser', { n: r.maxPerUserPerStream })}</span>{/if}
            {#if r.globalCooldownEnabled}<span class="tag">{t('channelpoints.chipCooldown', { n: r.globalCooldownSeconds })}</span>{/if}
            {#if r.isUserInputRequired}<span class="tag">{t('channelpoints.chipInput')}</span>{/if}
            {#if r.onRedeem === 'cancel'}<span class="tag">{t('channelpoints.chipRefund')}</span>{/if}
          </span>
        </span>
        <span class="resp">
          {#if r.action === 'chat'}
            {r.message || '{user} redeemed {reward}!'}
          {:else}
            <span class="silent">{t('channelpoints.chipSilent')}</span>
          {/if}
        </span>
        <span class="cost" title={t('channelpoints.fieldCost')}>{r.cost.toLocaleString()}</span>
      </span>
    {/snippet}
    {#snippet actions()}
      <form method="POST" action="?/update" use:enhance={toggleSubmit}>
        <input type="hidden" name="reward" value={togglePayload} />
        <Switch type="submit" checked={r.isEnabled} label={t('channelpoints.toggleAria', { name: r.title })} />
      </form>
      <button class="mini" type="button" aria-label={t('channelpoints.deleteAria', { name: r.title })} onclick={onDelete}>
        <Icon name="trash" size={15} />
      </button>
    {/snippet}
  </ManagementRow>
</div>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(150px, 1fr) minmax(0, 1.6fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .reward { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .reward-name {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13.5px;
    color: var(--bb-white);
    min-width: 0;
  }
  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: 6px;
    background: color-mix(in srgb, var(--sw) 24%, transparent);
    border: 1px solid color-mix(in srgb, var(--sw) 55%, transparent);
    color: color-mix(in srgb, var(--sw) 75%, white);
  }
  .swatch :global(svg) { stroke-width: 1.8; }
  .hidden-tag {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 9.5px;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--bb-muted);
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-radius: var(--bb-radius-pill, 100px);
    padding: 1px 8px;
  }
  .tags { display: flex; flex-wrap: wrap; gap: 4px; }
  .tag {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-radius: 8px;
    padding: 1px 6px;
    white-space: nowrap;
  }
  .resp {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .silent { opacity: 0.6; font-style: italic; }
  .cost {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .mini {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: none;
    color: var(--bb-muted);
    cursor: pointer;
  }
  .mini:hover { color: #cf8a78; border-color: rgba(176, 90, 70, 0.4); }
  .mini:focus-visible { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }

  @media (max-width: 760px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'reward'
        'resp'
        'cost';
      row-gap: 4px;
    }
    .idx { display: none; }
    .reward { grid-area: reward; }
    .resp { grid-area: resp; white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .cost { grid-area: cost; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
