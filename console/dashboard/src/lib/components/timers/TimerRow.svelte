<script lang="ts">
  // One ledger line in the timers deck. Selecting a row loads it into the
  // page's inspector pane; the row itself owns only the quick actions
  // (pause/resume toggle, delete) — the page passes the enhance handlers so
  // all optimistic-UI state lives in one place. Mirrors RewardRow.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, getI18n, type TimerDef } from '@bagel/shared';

  const { t } = getI18n();

  let {
    timer,
    index = undefined as number | undefined,
    expanded = false,
    onExpand,
    onDelete,
    toggleSubmit
  }: {
    timer: TimerDef;
    index?: number;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
    toggleSubmit: SubmitFunction;
  } = $props();

  const r = $derived(timer);
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');

  // The quick toggle posts the full timer with enabled flipped; the server
  // treats an update as authoritative, so nothing else changes.
  const togglePayload = $derived(JSON.stringify({ ...r, enabled: !r.enabled }));

  // formatInterval renders whole hours/minutes cleanly and falls back to raw
  // seconds only for the (dashboard-disallowed but defensively handled) case
  // of a value that isn't a whole minute.
  function formatInterval(seconds: number): string {
    if (seconds > 0 && seconds % 3600 === 0) return `${seconds / 3600}h`;
    if (seconds > 0 && seconds % 60 === 0) return `${seconds / 60}m`;
    return `${seconds}s`;
  }

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div class="row-shell reveal {expanded ? 'selected' : ''} {r.enabled ? '' : 'off'}" style="--i: {(index ?? 1) - 1}">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="trow" role="button" tabindex="0" aria-pressed={expanded} onclick={onExpand} onkeydown={rowKey}>
    {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}

    <span class="msg">
      <span class="msg-text">
        <span class="swatch"><Icon name="clock" size={11} /></span>
        {r.message}
        {#if !r.enabled}<span class="hidden-tag">{t('timers.hiddenTag')}</span>{/if}
      </span>
      <span class="tags">
        <span class="tag">{t('timers.chipInterval', { n: formatInterval(r.intervalSeconds) })}</span>
      </span>
    </span>

    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span class="row-act" onclick={(e) => e.stopPropagation()}>
      <form method="POST" action="?/update" use:enhance={toggleSubmit}>
        <input type="hidden" name="timer" value={togglePayload} />
        <button class="toggle {r.enabled ? 'on' : ''}" type="submit" aria-label={t('timers.toggleAria')}></button>
      </form>
      <button class="mini" type="button" aria-label={t('timers.deleteAria')} onclick={onDelete}>
        <Icon name="trash" size={15} />
      </button>
    </span>
  </div>
</div>

<style>
  /* Ledger line: hairline baselines, index number, selection keyed by a faint
     tan tint — matches RewardRow/CommandRow. */
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected { background: rgba(201, 168, 124, 0.05); }
  .row-shell.off .trow > :not(.row-act) { opacity: 0.5; }

  .trow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
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

  .msg { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .msg-text {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: 6px;
    background: color-mix(in srgb, var(--bb-tan, #c9a87c) 24%, transparent);
    border: 1px solid color-mix(in srgb, var(--bb-tan, #c9a87c) 55%, transparent);
    color: color-mix(in srgb, var(--bb-tan, #c9a87c) 75%, white);
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
    flex: none;
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

  .row-act { display: inline-flex; align-items: center; gap: 6px; }

  @media (max-width: 760px) {
    .trow {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'msg act'
        'msg act';
      row-gap: 4px;
    }
    .idx { display: none; }
    .msg { grid-area: msg; }
    .msg-text { white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .row-act { grid-area: act; flex-direction: column; gap: 4px; }
    .row-act form { display: flex; align-items: center; justify-content: center; min-width: 44px; min-height: 44px; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
