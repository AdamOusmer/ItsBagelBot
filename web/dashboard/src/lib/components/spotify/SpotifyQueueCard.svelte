<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Card, EmptyState, getI18n } from '@bagel/kit';
  import type { QueueView } from '$lib/server/songqueue-view';

  const TICK_MS = 5000;

  let {
    queue,
    updatedAt,
    refreshing = false,
    srEnabled,
    redeemEnabled,
    onRefresh,
    onEnableSr
  }: {
    queue: QueueView | null;
    updatedAt: number;
    refreshing?: boolean;
    srEnabled: boolean;
    redeemEnabled: boolean;
    onRefresh: () => void;
    onEnableSr: () => void;
  } = $props();

  const { t, locale } = getI18n();

  let now = $state(Date.now());
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), TICK_MS);
    return () => clearInterval(id);
  });

  const updatedLabel = $derived.by(() => {
    const seconds = Math.max(0, Math.floor((now - updatedAt) / 1000));
    const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
    const when =
      seconds < 60
        ? rtf.format(-Math.floor(seconds / 5) * 5, 'second')
        : rtf.format(-Math.floor(seconds / 60), 'minute');
    return t('spotify.queueUpdated', { when });
  });

  const upRows = $derived.by(() => {
    const seen = new Map<string, number>();
    return (queue?.up ?? []).map((row) => {
      const n = (seen.get(row.tid) ?? 0) + 1;
      seen.set(row.tid, n);
      return { ...row, key: `${row.tid}#${n}` };
    });
  });

  const emptyBody = $derived(
    srEnabled && redeemEnabled
      ? t('spotify.queueEmptyBoth')
      : srEnabled
        ? t('spotify.queueEmptySr')
        : redeemEnabled
          ? t('spotify.queueEmptyRedeem')
          : t('spotify.queueEmptyOff')
  );
  const isEmpty = $derived(!queue?.current && upRows.length === 0);
</script>

{#snippet track(row: { title: string; artists: string; requester: string })}
  <strong>{row.title}</strong>
  {#if row.artists}<span class="muted"> · {row.artists}</span>{/if}
  {#if row.requester}<span class="muted"> ({t('spotify.queueAskedBy')} {row.requester})</span>{/if}
{/snippet}

<Card>
  <div class="queue-head">
    <h2 class="queue-title">{t('spotify.queueTitle')}</h2>
    <span class="queue-stamp" role="status">{refreshing ? t('spotify.queueRefreshing') : updatedLabel}</span>
    <Button variant="ghost" type="button" loading={refreshing} onclick={onRefresh}>{t('spotify.queueRefresh')}</Button>
  </div>
  {#if queue?.current}
    <p class="queue-now">
      <span class="queue-label">{t('spotify.queueNow')}</span>
      {@render track(queue.current)}
    </p>
  {/if}
  {#if upRows.length}
    <ol class="queue-list">
      {#each upRows as row (row.key)}
        <li>{@render track(row)}</li>
      {/each}
    </ol>
  {:else if isEmpty}
    <EmptyState title={t('spotify.queueEmpty')} body={emptyBody}>
      {#if !srEnabled && !redeemEnabled}
        <Button variant="secondary" type="button" onclick={onEnableSr}>{t('spotify.queueEnableSr')}</Button>
      {/if}
    </EmptyState>
  {/if}
</Card>

<style>
  .queue-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .queue-title { margin: 0; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .queue-stamp { margin-left: auto; font-family: var(--bb-font-mono, monospace); font-size: 11.5px; color: var(--bb-muted); }
  .queue-now { margin: 6px 0 10px; }
  .queue-label { font-size: 0.82em; text-transform: uppercase; letter-spacing: 0.06em; opacity: 0.7; margin-right: 8px; }
  .queue-list { margin: 0; padding-left: 22px; display: grid; gap: 6px; }
  .muted { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; }
</style>
