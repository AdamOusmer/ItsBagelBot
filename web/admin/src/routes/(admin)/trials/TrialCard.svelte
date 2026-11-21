<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { durationLabel } from '#lib/duration';
  import type { TrialChannel } from '#lib/server/services';

  let { trial, current, locked, submit }: {
    trial: TrialChannel;
    current: boolean;
    locked: boolean;
    submit: SubmitFunction;
  } = $props();

  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const tone = {
    receiving: 'success',
    pending: 'warning',
    disabled: 'neutral',
    stopping: 'warning',
    promoted: 'success',
    removed: 'neutral',
    failed: 'danger'
  } as const;

  const displayName = $derived(trial.display_name?.trim());
  const name = $derived(displayName || trial.broadcaster_id);
  const stopping = $derived(trial.state === 'stopping');
  const metrics = $derived([
    { key: 'received', value: count(trial.received) },
    { key: 'decoded', value: count(trial.decoded) },
    { key: 'processed', value: count(trial.processed) },
    { key: 'blocked', value: count(trial.blocked_actions) },
    { key: 'failed', value: count(trial.failed), liveOnly: true },
    { key: 'retried', value: count(trial.retried), liveOnly: true },
    { key: 'latency', value: t('admin.trials.latency', {
      duration: durationLabel(trial.average_processing_latency_ns ?? 0)
    }) }
  ].filter((metric) => current || !metric.liveOnly));

  function count(value?: number): string {
    return number.format(value ?? 0);
  }
</script>

<Card as="li" style="min-block-size: var(--trial-card-size);">
  <div class="trial-heading">
    <Heading id={`trial-name-${trial.broadcaster_id}`} level={2} variant="title" class="trial-name" title={name}>
      <bb-fit><span data-fit>{name}</span></bb-fit>
    </Heading>
    <Tag bare class="trial-state">
      <StatusDot tone={tone[trial.state]} flat />
      <span class="trial-state-label"><bb-fit><span data-fit>{t(`admin.trials.state.${trial.state}`)}</span></bb-fit></span>
    </Tag>
  </div>

  <div class="trial-meta" class:with-id={displayName}>
    {#if displayName}
      <Text as="div" size="xs" tone="muted" mono>
        <bb-fit><span data-fit>{t('admin.trials.broadcasterId', { id: trial.broadcaster_id })}</span></bb-fit>
      </Text>
    {/if}
    <Text as="div" size="xs" tone="danger" truncate title={trial.error}>{trial.error}</Text>
  </div>

  <div class="trial-body">
    <ul class="bb-list trial-metrics">
      {#each metrics as metric (metric.key)}
        <Text as="li" size="xs" tone="muted">
          <bb-fit><span data-fit>{metric.key === 'latency'
            ? metric.value
            : t(`admin.trials.${metric.key}`, { count: metric.value })}</span></bb-fit>
        </Text>
      {/each}
    </ul>

    <div class="trial-actions" class:concealed={!current} inert={!current} aria-hidden={!current}>
      <form method="POST" action="?/set_enabled" use:enhance={submit} class="trial-toggle">
        <input type="hidden" name="broadcaster_id" value={trial.broadcaster_id} />
        <input type="hidden" name="enabled" value={trial.enabled ? 'false' : 'true'} />
        <Text as="span" size="sm" aria-hidden="true" class="trial-switch-label">
          <bb-fit><span data-fit>{t(trial.enabled ? 'admin.trials.on' : 'admin.trials.off')}</span></bb-fit>
        </Text>
        <Switch
          type="submit"
          checked={trial.enabled}
          label={t('admin.trials.toggleLabel', { name })}
          aria-disabled={locked || stopping}
          class="trial-switch"
          onclick={(event) => { if (locked || stopping) event.preventDefault(); }}
        />
      </form>

      <form
        method="POST" action="?/remove" use:enhance={submit}
        class="trial-remove" class:concealed={stopping} inert={stopping} aria-hidden={stopping}
      >
        <input type="hidden" name="broadcaster_id" value={trial.broadcaster_id} />
        <Button type="submit" variant="ghost" tone="danger" block aria-disabled={locked}
          aria-describedby={`trial-name-${trial.broadcaster_id}`}>
          <bb-fit><span data-fit>{t('admin.trials.remove')}</span></bb-fit>
        </Button>
      </form>
    </div>
  </div>
</Card>

<style>
  .trial-heading {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--bb-space-2);
  }

  .trial-heading :global(.trial-name) {
    min-inline-size: 0;
  }

  .trial-heading :global(.trial-state) {
    flex: none;
    inline-size: 7.25rem;
    justify-content: end;
  }

  .trial-state-label {
    min-inline-size: 0;
  }

  .trial-meta {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-2);
    min-block-size: 1.2rem;
  }

  .trial-meta.with-id {
    grid-template-columns: minmax(0, min(10rem, 50%)) minmax(0, 1fr);
  }

  .trial-body {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-3);
    margin-block-start: var(--bb-space-2);
  }

  .trial-metrics {
    flex: 1;
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-1) var(--bb-space-3);
    align-items: center;
    font-variant-numeric: tabular-nums;
  }

  .trial-actions {
    display: flex;
    align-items: center;
    justify-content: end;
    gap: var(--bb-space-2);
    flex: none;
  }

  .trial-toggle {
    display: flex;
    align-items: center;
    gap: var(--bb-space-1);
  }

  .trial-toggle :global(.trial-switch-label) {
    inline-size: 4.25rem;
    text-align: end;
  }

  .trial-toggle :global(.trial-switch[aria-disabled="true"]) {
    cursor: default;
    opacity: 0.55;
  }

  .trial-remove {
    inline-size: 6rem;
  }

  .trial-actions,
  .trial-remove {
    transition: opacity var(--bb-dur-fast) ease;
  }

  .concealed {
    opacity: 0;
    pointer-events: none;
  }

  @media (max-width: 980px) {
    .trial-body {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
    }

    .trial-metrics {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }

  @media (max-width: 680px) {
    .trial-metrics {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
