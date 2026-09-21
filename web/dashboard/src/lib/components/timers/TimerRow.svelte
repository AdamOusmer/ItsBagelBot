<script lang="ts">
  import { Button } from '@bagel/kit';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One ledger line in the timers deck, built on the shared ManagementRow so the
  // clickable primary is a real button and the quick actions (pause/resume
  // switch, delete) are siblings of it, never nested inside it. The page passes
  // the enhance handler so all optimistic state lives in one place.
  //
  // The row spells out both the schedule and the active/paused state as TEXT, so
  // neither needs opening the timer to learn and neither is conveyed by colour
  // alone: the schedule value carries an bb-sr-only "Repeat every" prefix, and the
  // state pill reads "Active"/"Paused" with colour only tinting it.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, ManagementRow, Switch, getI18n, fmtDate, type TimerDef } from '@bagel/kit';

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

  // Status pills (docs/specs/timer-conditions.md §7): read nothing when a
  // field is off (D11). Ended takes the until pill's place once endsAt has
  // passed (D6): the stop and the date it will happen / has happened are the
  // same fact, so showing both would repeat it.
  const ended = $derived.by(() => {
    if (!r.endsAt) return false;
    const t = Date.parse(r.endsAt);
    return !Number.isNaN(t) && t <= Date.now();
  });
  const untilLabel = $derived(
    r.endsAt && !ended ? fmtDate(r.endsAt, { parts: { month: 'short', day: 'numeric' } }) : ''
  );
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
        <span class="msg-text">{r.message}</span>
      </span>
      <!-- Metadata as labelled TEXT (no title tooltips): schedule value with an
           bb-sr-only prefix, and the state pill spelling out Active / Paused. -->
      <span class="meta">
        <span class="m-sched">
          <span class="bb-sr-only">{t('timers.fieldInterval')} </span>
          <span class="sched-val">{schedule}</span>
        </span>
        <span class="m-state bb-tag {r.enabled ? 'bb-tag--live' : 'bb-tag--quiet'}">
          <i class="bb-mark {r.enabled ? '' : 'bb-mark--hollow'}" aria-hidden="true"></i>
          {r.enabled ? t('timers.active') : t('timers.hiddenTag')}
        </span>
        {#if r.minChatLines > 0}
          <span class="m-pill bb-tag bb-tag--bare">{t('timers.pillMinLines', { n: r.minChatLines })}</span>
        {/if}
        {#if r.maxFiresPerStream > 0}
          <span class="m-pill bb-tag bb-tag--bare">{t('timers.pillMaxFires', { n: r.maxFiresPerStream })}</span>
        {/if}
        {#if ended}
          <span class="m-pill bb-tag bb-tag--bare">{t('timers.pillEnded')}</span>
        {:else if untilLabel}
          <span class="m-pill bb-tag bb-tag--bare">{t('timers.pillUntil', { date: untilLabel })}</span>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <form method="POST" action="?/update" use:enhance={toggleSubmit}>
      <input type="hidden" name="timer" value={togglePayload} />
      <Switch type="submit" checked={r.enabled} label={t('timers.toggleAria', { name: r.message })} />
    </form>
    <Button variant="icon" size="sm" class="delete-action" danger type="button" aria-label={t('timers.deleteAria', { name: r.message })} onclick={onDelete} ><Icon name="trash" size={15} /></Button>
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

  /* Metadata block: schedule value, state pill, gate/stop status pills, read
     as row text. Wraps on its own (unlike .prow's grid) since the pill count
     is per-timer and unbounded. */
  .meta { display: inline-flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px 12px; }
  .sched-val {
    font-family: var(--bb-font-mono);
    font-size: 13.5px;
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  /* Was a tinted outlined pill. Still TEXT-first with colour only tinting;
     the shape is now the global live/quiet label plus a solid/hollow mark, so
     Active vs Paused survives without colour. */
  .m-state, .m-pill { flex: none; }

  :global(.delete-action) { width: 32px; height: 32px; min-height: 32px; }

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
    :global(.delete-action) { min-width: 44px; min-height: 44px; }
  }
</style>
