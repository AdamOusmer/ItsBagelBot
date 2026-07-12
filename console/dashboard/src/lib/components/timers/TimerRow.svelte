<script lang="ts">
  // One ledger line in the timers deck, built on the shared ManagementRow so the
  // clickable primary is a real button and the quick actions (pause/resume
  // switch, delete) are siblings of it — never nested inside it. The page passes
  // the enhance handler so all optimistic state lives in one place.
  //
  // The row spells out both the schedule and the active/paused state as TEXT, so
  // neither needs opening the timer to learn and neither is conveyed by colour
  // alone: the schedule value carries an sr-only "Repeat every" prefix, and the
  // state pill reads "Active"/"Paused" with colour only tinting it.
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

  // Visible schedule summary as text: whole hours read as "N h", everything else
  // as whole minutes. The wire value is whole seconds and the editor only writes
  // whole minutes, so a non-whole-minute value is a defensive fallback.
  const schedule = $derived.by(() => {
    const s = r.intervalSeconds;
    if (s > 0 && s % 3600 === 0) return `${s / 3600} h`;
    return `${Math.max(1, Math.round(s / 60))} min`;
  });
</script>

<!-- No `disabled` prop: a paused timer is conveyed by the state pill and the
     switch, not by dimming the disclosure text (opacity would fail 4.5:1). -->
<ManagementRow
  selected={expanded}
  {expanded}
  controls="timer-editor"
  onselect={onExpand}
>
  {#snippet primary()}
    <span class="prow">
      {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
      <span class="msg">
        <span class="swatch" aria-hidden="true"><Icon name="clock" size={11} /></span>
        <span class="msg-text">{r.message}</span>
      </span>
      <!-- Metadata as labelled TEXT (no title tooltips): schedule value with an
           sr-only prefix, and the state pill spelling out Active / Paused. -->
      <span class="meta">
        <span class="m-sched">
          <span class="sr-only">{t('timers.fieldInterval')} </span>
          <span class="sched-val">{schedule}</span>
        </span>
        <span class="m-state {r.enabled ? 'on' : 'off'}">
          {r.enabled ? t('timers.active') : t('timers.hiddenTag')}
        </span>
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <form method="POST" action="?/update" use:enhance={toggleSubmit}>
      <input type="hidden" name="timer" value={togglePayload} />
      <Switch type="submit" checked={r.enabled} label={t('timers.toggleAria', { name: r.message })} />
    </form>
    <button class="mini" type="button" aria-label={t('timers.deleteAria', { name: r.message })} onclick={onDelete}>
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

  /* Metadata block: schedule value + state pill, read as row text. */
  .meta { display: inline-flex; align-items: center; justify-content: flex-end; gap: 12px; }
  .sched-val {
    font-family: var(--bb-font-mono);
    font-size: 13.5px;
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  /* State pill: TEXT is the primary cue, colour only tints. Uppercased so the
     reused "Active" / "Paused" strings read as a consistent pair. */
  .m-state {
    flex: none;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 9.5px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    border-radius: var(--bb-radius-pill, 100px);
    padding: 2px 9px;
    white-space: nowrap;
  }
  .m-state.on { color: var(--bb-green-glow, #7fd4a3); border: 1px solid rgba(82, 183, 136, 0.4); }
  .m-state.off { color: var(--bb-muted); border: 1px solid var(--rule, rgba(240, 236, 228, 0.12)); }

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

  /* Narrow: stack the schedule + state under the message and keep 44px targets;
     reflows cleanly down to 320px. */
  @media (max-width: 760px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'msg'
        'meta';
      row-gap: 4px;
    }
    .idx { display: none; }
    .msg { grid-area: msg; }
    .meta { grid-area: meta; justify-content: flex-start; flex-wrap: wrap; gap: 8px 12px; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
