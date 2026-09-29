<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Card, EmptyState, Heading, Text } from '@bagel/ui/svelte';
  import { getI18n } from '@bagel/kit';
  import type { QueueView } from '$lib/server/songqueue-view';

  const TICK_MS = 1000;

  let {
    queue,
    updatedAt,
    refreshing = false,
    skipping = false,
    srEnabled,
    redeemEnabled,
    onRefresh,
    onSkip,
    onEnableSr
  }: {
    queue: QueueView | null;
    updatedAt: number;
    refreshing?: boolean;
    skipping?: boolean;
    srEnabled: boolean;
    redeemEnabled: boolean;
    onRefresh: () => void;
    onSkip: () => void;
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
    return t('spotify.queue.updated', { when });
  });

  const secondsFormat = $derived(new Intl.NumberFormat(locale, { minimumIntegerDigits: 2, useGrouping: false }));
  const clock = (ms: number) => {
    const total = Math.floor(ms / 1000);
    return `${Math.floor(total / 60)}:${secondsFormat.format(total % 60)}`;
  };
  const progressLabel = $derived.by(() => {
    const progress = queue?.progress;
    if (!progress) return null;
    const drift = progress.playing ? Math.max(0, now - updatedAt) : 0;
    const elapsed = clock(Math.min(progress.positionMs + drift, progress.durationMs));
    const duration = clock(progress.durationMs);
    return { text: `${elapsed} / ${duration}`, aria: t('spotify.queue.progressLabel', { elapsed, duration }) };
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
      ? t('spotify.queue.emptyBoth')
      : srEnabled
        ? t('spotify.queue.emptySr')
        : redeemEnabled
          ? t('spotify.queue.emptyRedeem')
          : t('spotify.queue.emptyOff')
  );
  const isEmpty = $derived(!queue?.current && upRows.length === 0);
</script>

{#snippet track(row: { title: string; artists: string; requester: string })}
  <strong>{row.title}</strong>
  {#if row.artists}<Text as="span" size="sm" tone="muted"> · {row.artists}</Text>{/if}
  {#if row.requester}<Text as="span" size="sm" tone="muted"> ({t('spotify.queue.askedBy')} {row.requester})</Text>{/if}
{/snippet}

<Card>
  <div class="queue-head">
    <Heading level={6} as="h2">{t('spotify.queue.title')}</Heading>
    <span class="queue-stamp" role="status"><Text as="span" size="xs" mono tone="muted">{refreshing ? t('spotify.queue.refreshing') : updatedLabel}</Text></span>
    <Button variant="ghost" type="button" busy={refreshing} onclick={onRefresh}>{t('spotify.queue.refresh')}</Button>
  </div>
  {#if queue?.current}
    <div class="queue-now">
      <span class="queue-label">{t('spotify.queue.now')}</span>
      <span class="queue-track">{@render track(queue.current)}</span>
      {#if progressLabel}
        <span class="queue-time" aria-label={progressLabel.aria}><Text as="span" size="xs" mono tone="muted">{progressLabel.text}</Text></span>
      {/if}
      <Button variant="secondary" type="button" busy={skipping} onclick={onSkip}>{t('spotify.queue.skip')}</Button>
    </div>
  {/if}
  {#if upRows.length}
    <ol class="queue-list">
      {#each upRows as row (row.key)}
        <li>{@render track(row)}</li>
      {/each}
    </ol>
  {:else if isEmpty}
    <EmptyState title={t('spotify.queue.empty')} body={emptyBody}>
      {#if !srEnabled && !redeemEnabled}
        <Button variant="secondary" type="button" onclick={onEnableSr}>{t('spotify.queue.enableSr')}</Button>
      {/if}
    </EmptyState>
  {/if}
</Card>

<style>
  .queue-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .queue-stamp { margin-left: auto; }
  .queue-now { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 6px 0 10px; }
  .queue-label { font-size: 0.82em; text-transform: uppercase; letter-spacing: 0.06em; opacity: 0.7; }
  .queue-track { flex: 1 1 200px; min-width: 0; }
  .queue-time { min-width: 11ch; text-align: right; font-variant-numeric: tabular-nums; }
  .queue-list { margin: 0; padding-left: 22px; display: grid; gap: 6px; }
</style>
