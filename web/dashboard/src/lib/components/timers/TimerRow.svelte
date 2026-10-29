<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
	import '@bagel/ui/styles/elements/typography.css';
  import { enhance, type SubmitFunction } from '$app/forms';
  import '@bagel/ui/styles/elements/row-body.css';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { getI18n, fmtDate, type TimerDef } from '@bagel/kit';
  import RowDeleteButton from '#lib/components/shared/RowDeleteButton.svelte';
  import { formatDuration } from '#lib/components/shared/duration';

  const { t, locale } = getI18n();

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

  const schedule = $derived(formatDuration(r.intervalSeconds, t, locale));

  const ended = $derived.by(() => {
    if (!r.endsAt) return false;
    const endsAt = Date.parse(r.endsAt);
    return !Number.isNaN(endsAt) && endsAt <= Date.now();
  });
  const untilLabel = $derived(
    r.endsAt && !ended ? fmtDate(r.endsAt, { parts: { month: 'short', day: 'numeric' } }) : ''
  );
</script>

<ManagementRow
  selected={expanded}
  {expanded}
  controls="timer-editor"
  onSelect={onExpand}
>
  {#snippet primary()}
    <span class="prow bb-row-body bb-row-body--stack">
      {#if idx}<span class="bb-row-body__idx" aria-hidden="true">{idx}</span>{/if}
      <span class="msg">
        <span class="bb-row-body__name">{r.message}</span>
      </span>
      <span class="bb-row-body__meta meta">
        <span class="m-sched">
          <span class="bb-sr-only">{t('timers.fieldInterval')} </span>
          <span class="bb-row-body__val">{schedule}</span>
        </span>
        <Tag class="m-state" tone={r.enabled ? 'live' : 'quiet'} mark={r.enabled ? 'solid' : 'hollow'}>
          {r.enabled ? t('timers.active') : t('timers.hiddenTag')}
        </Tag>
        {#if r.minChatLines > 0}
          <Tag class="m-pill" tone="bare">{t('timers.pillMinLines', { n: r.minChatLines, m: r.chatWindowMinutes })}</Tag>
        {/if}
        {#if r.allowOffline}
          <Tag class="m-pill" tone="bare">{t('timers.pillOffline')}</Tag>
        {/if}
        {#if r.maxFiresPerStream > 0}
          <Tag class="m-pill" tone="bare">{t('timers.pillMaxFires', { n: r.maxFiresPerStream })}</Tag>
        {/if}
        {#if ended}
          <Tag class="m-pill" tone="bare">{t('timers.pillEnded')}</Tag>
        {:else if untilLabel}
          <Tag class="m-pill" tone="bare">{t('timers.pillUntil', { date: untilLabel })}</Tag>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <form method="POST" action="?/update" use:enhance={toggleSubmit}>
      <input type="hidden" name="timer" value={togglePayload} />
      <Switch type="submit" checked={r.enabled} label={t('timers.toggleAria', { name: r.message })} />
    </form>
    <RowDeleteButton label={t('timers.deleteAria', { name: r.message })} onclick={onDelete} />
  {/snippet}
</ManagementRow>

<style>
  .prow { --row-cols: 28px minmax(0, 1fr) auto; }

  .msg { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }

  .meta { flex-wrap: wrap; gap: 8px 12px; }

  .meta :global(.m-state), .meta :global(.m-pill) { flex: none; }
</style>
