<script lang="ts">
  // One ledger line in the reward deck. Selecting a row loads it into the
  // page's inspector pane; the row itself owns only the quick actions
  // (show/hide toggle, delete) — the page passes the enhance handlers so all
  // optimistic-UI state lives in one place. Mirrors CommandRow.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, getI18n, type ChannelPointReward } from '@bagel/shared';

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

  // The quick toggle posts the full reward with the Twitch visibility flipped;
  // the server treats an update as authoritative, so nothing else changes.
  const togglePayload = $derived(JSON.stringify({ ...r, isEnabled: !r.isEnabled }));

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div class="row-shell reveal {expanded ? 'selected' : ''} {r.isEnabled ? '' : 'off'}" style="--i: {(index ?? 1) - 1}">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="trow" role="button" tabindex="0" aria-pressed={expanded} onclick={onExpand} onkeydown={rowKey}>
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

    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span class="row-act" onclick={(e) => e.stopPropagation()}>
      <form method="POST" action="?/update" use:enhance={toggleSubmit}>
        <input type="hidden" name="reward" value={togglePayload} />
        <button class="toggle {r.isEnabled ? 'on' : ''}" type="submit" aria-label={t('channelpoints.toggleAria', { name: r.title })}></button>
      </form>
      <button class="mini" type="button" aria-label={t('channelpoints.deleteAria', { name: r.title })} onclick={onDelete}>
        <Icon name="trash" size={15} />
      </button>
    </span>
  </div>
</div>

<style>
  /* Ledger line: hairline baselines, index number, selection keyed by a faint
     tan tint — matches CommandRow. */
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected { background: rgba(201, 168, 124, 0.05); }
  .row-shell.off .trow > :not(.row-act) { opacity: 0.5; }

  .trow {
    display: grid;
    grid-template-columns: 28px minmax(150px, 1fr) minmax(0, 1.6fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 12px 14px;
    cursor: pointer;
    user-select: none;
  }
  .trow:hover { background: rgba(201, 168, 124, 0.045); }
  .trow:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }
  .row-shell.selected .idx { color: var(--bb-tan); opacity: 1; }

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

  /* The reward's Twitch color, worn as a small gem chip. */
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

  .row-act { display: inline-flex; align-items: center; gap: 6px; }

  @media (max-width: 760px) {
    .trow {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'reward act'
        'resp act'
        'cost act';
      row-gap: 4px;
    }
    .idx { display: none; }
    .reward { grid-area: reward; }
    .resp { grid-area: resp; white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .cost { grid-area: cost; }
    .row-act { grid-area: act; flex-direction: column; gap: 4px; }
    .row-act form { display: flex; align-items: center; justify-content: center; min-width: 44px; min-height: 44px; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
