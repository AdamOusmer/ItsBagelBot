<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getAbortSignal } from 'svelte';
  import { applyAction, enhance, type SubmitFunction } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { livePoll } from '@bagel/kit/live-poll';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
  import type { TrialChannel, TrialSnapshot } from '#lib/server/services';
  import { applyStreamed } from '#lib/streamed-load';
  import type { PageProps } from './$types';
  import TrialCard from './TrialCard.svelte';

  const SNAPSHOT_URL = '/trials/snapshot';
  const REFRESH_MS = 5000;
  const FILTER_LABELS = {
    all: 'admin.trials.filterAll',
    current: 'admin.trials.filterCurrent',
    history: 'admin.trials.history'
  } as const;
  const EMPTY_LABELS = {
    all: 'admin.trials.empty',
    current: 'admin.trials.emptyCurrent',
    history: 'admin.trials.emptyHistory'
  } as const;
  type TrialFilter = keyof typeof FILTER_LABELS;
  type TrialActionResult = Parameters<typeof applyAction>[0];

  let { data, form }: PageProps = $props();
  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const failed = adminToastFailure(toast);
  let snapshot = $state.raw<TrialSnapshot>();
  let settledLoad = $state.raw<Promise<TrialSnapshot | null>>();
  let degraded = $state(false);
  let busy = $state(false);
  let filter = $state<TrialFilter>('all');
  let results: HTMLDivElement;

  const awaiting = $derived(settledLoad !== data.snapshot);
  const locked = $derived(awaiting || busy || degraded);
  const trials = $derived([...(snapshot?.trials ?? [])].sort((a, b) => a.broadcaster_id.localeCompare(b.broadcaster_id)));
  const current = $derived(trials.filter(isCurrent));
  const history = $derived(trials.filter((trial) => !isCurrent(trial)));
  const groups = $derived({ all: [...current, ...history], current, history });
  const activeCount = $derived(snapshot?.active_count ?? current.length);
  const maxChannels = $derived(snapshot?.max_channels ?? 30);
  const atCapacity = $derived(activeCount >= maxChannels);
  const options = $derived((Object.keys(FILTER_LABELS) as TrialFilter[]).map((value) => ({
    value,
    label: t(FILTER_LABELS[value]),
    count: snapshot ? number.format(groups[value].length) : '–'
  })));

  $effect(() => {
    const pending = data.snapshot;
    applyStreamed(pending.catch(() => null), (next) => {
      applySnapshot(next);
      settledLoad = pending;
    });
  });

  $effect(() => {
    if (awaiting || busy) return;
    const signal = getAbortSignal();
    return livePoll(() => refresh(signal), {
      firstDelayMs: REFRESH_MS,
      delayMs: () => REFRESH_MS,
      hiddenDelayMs: 60_000,
      timeoutMs: Number.POSITIVE_INFINITY,
      refreshOnVisible: true
    });
  });

  function isCurrent(trial: TrialChannel): boolean {
    return trial.state !== 'removed' && trial.state !== 'promoted';
  }

  function applySnapshot(next: TrialSnapshot | null) {
    degraded = next === null;
    if (next) snapshot = next;
  }

  async function refresh(signal: AbortSignal): Promise<boolean> {
    if (document.hidden) return false;
    try {
      const response = await fetch(SNAPSHOT_URL, { signal, cache: 'no-store' });
      if (!response.ok) throw new Error('Trial snapshot unavailable');
      const body = (await response.json()) as { snapshot: TrialSnapshot };
      if (!signal.aborted) applySnapshot(body.snapshot);
    } catch {
      if (!signal.aborted) applySnapshot(null);
    }
    return false;
  }

  type ActionSubmission = Pick<Parameters<SubmitFunction>[0], 'action' | 'formElement' | 'submitter'>;

  async function finishAction(result: TrialActionResult, { action, formElement, submitter }: ActionSubmission) {
    if (result.type === 'redirect') return applyAction(result);

    const payload = actionPayload<{ ok?: boolean; notice?: string; error?: string }>(result);
    if (!payload?.ok) return failed(payload, t('admin.trials.action.unavailable'));

    formElement.reset();
    if (action.searchParams.has('/add') && filter === 'history') filter = 'current';
    if (action.searchParams.has('/remove') && submitter === document.activeElement) {
      results?.focus({ preventScroll: true });
    }
    if (payload.notice) toast('success', payload.notice);
    await invalidate(SNAPSHOT_URL).catch(() => applySnapshot(null));
  }

  const submit: SubmitFunction = ({ cancel, action, formElement, submitter }) => {
    const addingAtCapacity = action.searchParams.has('/add') && atCapacity;
    if (locked || addingAtCapacity) return cancel();
    busy = true;
    return async ({ result }) => {
      try {
        await finishAction(result, { action, formElement, submitter });
      } finally {
        busy = false;
      }
    };
  };
</script>

<section class="screen active trials">
  <PageHead description={t('admin.trials.description')}>
    <bb-fit><span data-fit>{t('admin.trials.titlePre')}<em>{t('admin.trials.titleEm')}</em></span></bb-fit>
  </PageHead>

  <Stack gap={3} style="margin-block-end: var(--bb-space-3);">
    <FilterTabs
      label={t('admin.trials.filterLabel')}
      aria-controls="trial-results"
      {options}
      value={filter}
      onValueChange={(value) => (filter = value as TrialFilter)}
      countWidth="5ch"
    />
    <form method="POST" action="?/add" use:enhance={submit} class="trial-add">
      <Label htmlFor="trial-broadcaster-id" class="trial-add-label">
        <bb-fit><span data-fit>{t('admin.trials.idLabel')}</span></bb-fit>
      </Label>
      <Input
        id="trial-broadcaster-id"
        name="broadcaster_id"
        inputmode="numeric"
        pattern="[1-9][0-9]*"
        maxlength={20}
        autocomplete="off"
        aria-describedby="trial-help"
        readonly={busy}
        fill
        mono
        required
      />
      <Button type="submit" aria-disabled={locked || atCapacity} block>
        <bb-fit><span data-fit>{t('admin.trials.add')}</span></bb-fit>
      </Button>
    </form>
  </Stack>

  <div class="trial-summary">
    <div class="trial-capacity" class:concealed={degraded} aria-hidden={degraded}>
      <Text size="sm" mono style="inline-size: min(14.5rem, 100%); min-inline-size: 0">
        {#if snapshot}
          <bb-fit><span data-fit>{t('admin.trials.count', {
            count: number.format(activeCount),
            max: number.format(maxChannels)
          })}</span></bb-fit>
        {:else}
          <Skeleton width="11rem" />
        {/if}
      </Text>
      <Text id="trial-help" size="sm" tone="muted">{t('admin.trials.note')}</Text>
    </div>
    <div class="trial-alert" class:concealed={!degraded} aria-hidden={!degraded}>
      <AlertBanner flush>{t('admin.trials.degraded')}</AlertBanner>
    </div>
  </div>

  {#if form?.error}<AlertBanner>{form.error}</AlertBanner>{/if}
  {#if form?.notice}<AlertBanner tone="success" role="status">{form.notice}</AlertBanner>{/if}

  <div
    id="trial-results"
    class="trial-results"
    role="region"
    aria-label={t('admin.trials.titlePre') + t('admin.trials.titleEm')}
    aria-busy={awaiting || busy}
    tabindex="-1"
    bind:this={results}
  >
    {#if !snapshot && !degraded}
      <Stack gap={3} aria-hidden="true">
        <Skeleton variant="block" height="var(--trial-card-size)" />
        <Skeleton variant="block" height="var(--trial-card-size)" />
      </Stack>
    {:else if groups[filter].length}
      <Stack as="ul" gap={3} class="bb-list" aria-label={t(FILTER_LABELS[filter])}>
        {#each groups[filter] as trial (trial.broadcaster_id)}
          <TrialCard {trial} {locked} {submit} current={isCurrent(trial)} />
        {/each}
      </Stack>
    {:else if snapshot}
      <EmptyState title={t(EMPTY_LABELS[filter])} />
    {/if}
  </div>
</section>

<style>
  .trials {
    --trial-card-size: 8rem;
  }

  .trial-add {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 8.75rem;
    align-items: stretch;
    gap: var(--bb-space-1) var(--bb-space-2);
    inline-size: min(100%, 24rem);
  }

  .trial-add :global(.trial-add-label) {
    grid-column: 1 / -1;
    min-inline-size: 0;
  }

  .trial-summary {
    display: grid;
    align-items: center;
    margin-block-end: var(--bb-space-3);
  }

  .trial-capacity,
  .trial-alert {
    grid-area: 1 / 1;
    min-inline-size: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .trial-capacity {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    column-gap: var(--bb-space-5);
    row-gap: var(--bb-space-1);
    padding-block: var(--bb-space-2);
  }

  .concealed {
    opacity: 0;
    pointer-events: none;
  }

  .trial-results:focus-visible {
    outline: 2px solid var(--bb-focus);
    outline-offset: var(--bb-space-1);
  }

  @media (max-width: 980px) {
    .trials {
      --trial-card-size: 12rem;
    }
  }

  @media (max-width: 680px) {
    .trials {
      --trial-card-size: 12rem;
    }

    .trial-add {
      grid-template-columns: minmax(0, 1fr) 7.75rem;
    }
  }
</style>
