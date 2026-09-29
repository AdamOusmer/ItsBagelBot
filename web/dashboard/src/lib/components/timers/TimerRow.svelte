<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, IconButton, ManagementRow, Switch, Tag, getI18n, fmtDate, type TimerDef } from '@bagel/kit';

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

  const schedule = $derived.by(() => {
    const s = r.intervalSeconds;
    if (s > 0 && s % 3600 === 0) return `${s / 3600} h`;
    return `${Math.max(1, Math.round(s / 60))} min`;
  });

  const ended = $derived.by(() => {
    if (!r.endsAt) return false;
    const t = Date.parse(r.endsAt);
    return !Number.isNaN(t) && t <= Date.now();
  });
  const untilLabel = $derived(
    r.endsAt && !ended ? fmtDate(r.endsAt, { parts: { month: 'short', day: 'numeric' } }) : ''
  );
</script>

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
      <span class="meta">
        <span class="m-sched">
          <span class="bb-sr-only">{t('timers.fieldInterval')} </span>
          <span class="sched-val">{schedule}</span>
        </span>
        <Tag tone={r.enabled ? 'live' : 'quiet'} mark={r.enabled ? 'solid' : 'hollow'}>
          {r.enabled ? t('timers.active') : t('timers.hiddenTag')}
        </Tag>
        {#if r.minChatLines > 0}
          <Tag tone="bare">{t('timers.pillMinLines', { n: r.minChatLines })}</Tag>
        {/if}
        {#if r.maxFiresPerStream > 0}
          <Tag tone="bare">{t('timers.pillMaxFires', { n: r.maxFiresPerStream })}</Tag>
        {/if}
        {#if ended}
          <Tag tone="bare">{t('timers.pillEnded')}</Tag>
        {:else if untilLabel}
          <Tag tone="bare">{t('timers.pillUntil', { date: untilLabel })}</Tag>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <form method="POST" action="?/update" use:enhance={toggleSubmit}>
      <input type="hidden" name="timer" value={togglePayload} />
      <Switch type="submit" checked={r.enabled} label={t('timers.toggleAria', { name: r.message })} />
    </form>
    <span class="del">
      <IconButton size="sm" danger label={t('timers.deleteAria', { name: r.message })} onclick={onDelete}><Icon name="trash" size={15} /></IconButton>
    </span>
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); color: var(--bb-muted); opacity: 0.55; }

  .msg { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
  .msg-text {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .meta { display: inline-flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px 12px; }
  .sched-val {
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-sm);
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .del { display: inline-flex; --btn-icon-min-size: 32px; }

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
    .del { --btn-icon-min-size: 44px; }
  }
</style>
