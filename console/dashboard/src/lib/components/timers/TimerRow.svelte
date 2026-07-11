<script lang="ts">
  // One ledger line in the timers deck, built on the shared ManagementRow so the
  // clickable primary is a real button and the quick actions (pause/resume
  // switch, delete) are siblings of it — never nested inside it. The page passes
  // the enhance handler so all optimistic state lives in one place.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, ManagementRow, Switch, getI18n, type TimerDef } from '@bagel/shared';

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
  const togglePayload = $derived(JSON.stringify({ ...r, enabled: !r.enabled }));

  function formatInterval(seconds: number): string {
    if (seconds > 0 && seconds % 3600 === 0) return `${seconds / 3600}h`;
    if (seconds > 0 && seconds % 60 === 0) return `${seconds / 60}m`;
    return `${seconds}s`;
  }
</script>

<ManagementRow
  selected={expanded}
  {expanded}
  ariaLabel={r.message}
  disabled={!r.enabled}
  onselect={onExpand}
>
  {#snippet primary()}
    <span class="prow">
      {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
      <span class="msg">
        <span class="swatch"><Icon name="clock" size={11} /></span>
        <span class="msg-text">{r.message}</span>
        {#if !r.enabled}<span class="hidden-tag">{t('timers.hiddenTag')}</span>{/if}
      </span>
      <span class="meta" title={t('timers.fieldInterval')}>
        <span class="interval">{formatInterval(r.intervalSeconds)}</span>
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <form method="POST" action="?/update" use:enhance={toggleSubmit}>
      <input type="hidden" name="timer" value={togglePayload} />
      <Switch type="submit" checked={r.enabled} label={t('timers.toggleAria')} />
    </form>
    <button class="mini" type="button" aria-label={t('timers.deleteAria')} onclick={onDelete}>
      <Icon name="trash" size={15} />
    </button>
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .msg { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
  .msg-text {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
    background: color-mix(in srgb, var(--bb-tan, #c9a87c) 24%, transparent);
    border: 1px solid color-mix(in srgb, var(--bb-tan, #c9a87c) 55%, transparent);
    color: color-mix(in srgb, var(--bb-tan, #c9a87c) 75%, white);
  }
  .swatch :global(svg) { stroke-width: 1.8; }
  .hidden-tag {
    flex: none;
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

  .meta { display: inline-flex; align-items: center; justify-content: flex-end; }
  .interval {
    font-family: var(--bb-font-mono);
    font-size: 13.5px;
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
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'msg meta';
      row-gap: 4px;
    }
    .idx { display: none; }
    .msg { grid-area: msg; }
    .meta { grid-area: meta; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
