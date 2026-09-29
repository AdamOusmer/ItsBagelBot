<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import type { TrialChannel, TrialSnapshot } from '$lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { TrialsBundle } from './+page.server';
  import { durationLabel } from '$lib/duration';

  let { data, form } = $props();
  const { t } = getI18n();
  let snapshot = $state<TrialSnapshot | null>(null);
  const active = $derived(snapshot?.trials.filter((row) => row.state !== 'removed' && row.state !== 'promoted') ?? []);
  const history = $derived(snapshot?.trials.filter((row) => row.state === 'removed' || row.state === 'promoted') ?? []);
  const activeCount = $derived(snapshot?.active_count ?? active.length);
  const maxChannels = $derived(snapshot?.max_channels ?? 30);
  let degraded = $state(false);
  let broadcasterId = $state('');
  $effect(() => {
    let alive = true;
    data.bundle.then((bundle: TrialsBundle) => {
      if (!alive) return;
      snapshot = bundle.snapshot;
      degraded = bundle.degraded;
    });
    return () => { alive = false; };
  });

  const POLL_MS = 5000;
  const HIDDEN_POLL_MS = 15_000;

  async function refresh(): Promise<boolean> {
    try {
      const response = await fetch('/trials/snapshot');
      if (!response.ok) throw new Error('unavailable');
      const body = (await response.json()) as { snapshot: TrialSnapshot };
      snapshot = body.snapshot;
      degraded = false;
    } catch {
      degraded = true;
    }
    return false;
  }

  onMount(() =>
    livePoll(refresh, {
      firstDelayMs: POLL_MS,
      delayMs: () => POLL_MS,
      timeoutMs: Number.POSITIVE_INFINITY,
      hiddenDelayMs: HIDDEN_POLL_MS,
      refreshOnVisible: true
    })
  );

  const STATE_TONE: Partial<Record<TrialChannel['state'], 'positive' | 'danger'>> = {
    receiving: 'positive',
    failed: 'danger'
  };

  const byId = (a: TrialChannel, b: TrialChannel) => a.broadcaster_id.localeCompare(b.broadcaster_id);
  const sortedActive = $derived([...active].sort(byId));
  const sortedHistory = $derived([...history].sort(byId));

  const count = (value?: number) => String(value ?? 0);

  function trialName(trial: TrialChannel): string {
    return trial.display_name?.trim() || trial.broadcaster_id;
  }

  function trialStats(trial: TrialChannel, withFailures: boolean): string {
    const failures = withFailures
      ? [
          t('admin.trials.failed', { count: count(trial.failed) }),
          t('admin.trials.retried', { count: count(trial.retried) })
        ]
      : [];
    return [
      t('admin.trials.received', { count: count(trial.received) }),
      t('admin.trials.decoded', { count: count(trial.decoded) }),
      t('admin.trials.processed', { count: count(trial.processed) }),
      ...failures,
      t('admin.trials.blocked', { count: count(trial.blocked_actions) }),
      t('admin.trials.latency', { duration: durationLabel(trial.average_processing_latency_ns ?? 0) })
    ].join(' · ');
  }
</script>

{#snippet trialSummary(trial: TrialChannel, withFailures: boolean)}
  <Stack gap={1} align="start">
    <Heading level={6} as="h3">{trialName(trial)}</Heading>
    {#if trial.display_name?.trim()}<Text as="span" size="xs" tone="muted">{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</Text>{/if}
    <Tag tone={STATE_TONE[trial.state] ?? 'neutral'}>{t(`admin.trials.state.${trial.state}`)}</Tag>
    {#if trial.error}<Text as="span" tone="danger">{trial.error}</Text>{/if}
    <Text as="small" size="sm" tone="muted">{trialStats(trial, withFailures)}</Text>
  </Stack>
{/snippet}

<section class="screen active trials">
  <PageHead
    eyebrow={t('admin.trials.eyebrow')}
    title={t('admin.trials.title')}
    description={t('admin.trials.description', { max: String(maxChannels) })}
  />

  {#if degraded}<AlertBanner>{t('admin.trials.degraded')}</AlertBanner>{/if}
  {#if form?.error}<AlertBanner>{form.error}</AlertBanner>{/if}
  {#if form?.notice}<AlertBanner tone="warning" role="status">{form.notice}</AlertBanner>{/if}

  <Stack gap={4}>
    <form method="POST" action="?/add" class="trial-add">
      <Field label={t('admin.trials.idLabel')}>
        <Input
          name="broadcaster_id"
          type="text"
          inputmode="numeric"
          pattern="[1-9][0-9]*"
          maxlength={20}
          autocomplete="off"
          required
          bind:value={broadcasterId}
        />
      </Field>
      <Button type="submit" disabled={degraded || activeCount >= maxChannels}>{t('admin.trials.add')}</Button>
    </form>
    <Text tone="muted">{t('admin.trials.note', { max: String(maxChannels) })}</Text>

    {#if snapshot === null}
      <SkeletonStack rows={2} height="96px" />
    {:else}
      <Text tone="muted">{t('admin.trials.count', { count: String(activeCount), max: String(maxChannels) })}</Text>
      {#if active.length === 0}
        <EmptyState title={t('admin.trials.empty')} />
      {:else}
        <Stack as="ul" gap={3} class="bb-list">
          {#each sortedActive as trial (trial.broadcaster_id)}
            <Card as="li">
              <Cluster justify="between" gap={4}>
                {@render trialSummary(trial, true)}
                <Cluster gap={3}>
                  <Cluster as="form" gap={3} method="POST" action="?/set_enabled">
                    <Text as="span" size="sm" aria-hidden="true">{trial.enabled ? t('admin.trials.on') : t('admin.trials.off')}</Text>
                    <input type="hidden" name="broadcaster_id" value={trial.broadcaster_id} />
                    <input type="hidden" name="enabled" value={trial.enabled ? 'false' : 'true'} />
                    <Switch
                      type="submit"
                      checked={trial.enabled}
                      label={t('admin.trials.toggleLabel', { name: trialName(trial) })}
                      disabled={degraded || trial.state === 'stopping'}
                    />
                  </Cluster>
                  {#if trial.state !== 'stopping'}
                    <form method="POST" action="?/remove">
                      <input type="hidden" name="broadcaster_id" value={trial.broadcaster_id} />
                      <Button type="submit" tone="danger">{t('admin.trials.remove')}</Button>
                    </form>
                  {/if}
                </Cluster>
              </Cluster>
            </Card>
          {/each}
        </Stack>
      {/if}
      {#if history.length > 0}
        <Heading level={3} as="h2">{t('admin.trials.history')}</Heading>
        <Stack as="ul" gap={3} class="bb-list">
          {#each sortedHistory as trial (trial.broadcaster_id)}
            <Card as="li">{@render trialSummary(trial, false)}</Card>
          {/each}
        </Stack>
      {/if}
    {/if}
  </Stack>
</section>

<style>
  .trials {
    --card-pad: var(--bb-space-4);
  }
  .trial-add {
    --field-mb: 0;
    display: flex;
    align-items: end;
    flex-wrap: wrap;
    gap: var(--bb-space-3);
  }
</style>
