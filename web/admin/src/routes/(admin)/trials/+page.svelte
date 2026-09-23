<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import type { TrialSnapshot } from '$lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { TrialsBundle } from './+page.server';

  let { data, form } = $props();
  const { t } = getI18n();
  let snapshot = $state<TrialSnapshot | null>(null);
  const active = $derived(snapshot?.trials.filter((row) => row.state !== 'removed' && row.state !== 'promoted') ?? []);
  const history = $derived(snapshot?.trials.filter((row) => row.state === 'removed' || row.state === 'promoted') ?? []);
  const activeCount = $derived(snapshot?.active_count ?? active.length);
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

  onMount(() => {
    let running = false;
    const refresh = async () => {
      if (running || document.hidden) return;
      running = true;
      try {
        const response = await fetch('/trials/snapshot');
        if (!response.ok) throw new Error('unavailable');
        const body = (await response.json()) as { snapshot: TrialSnapshot };
        snapshot = body.snapshot;
        degraded = false;
      } catch {
        degraded = true;
      } finally {
        running = false;
      }
    };
    const interval = setInterval(refresh, 5000);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      clearInterval(interval);
      document.removeEventListener('visibilitychange', refresh);
    };
  });
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('admin.trials.eyebrow')}
    title={t('admin.trials.title')}
    description={t('admin.trials.description')}
  />

  {#if degraded}<AlertBanner>{t('admin.trials.degraded')}</AlertBanner>{/if}
  {#if form?.error}<AlertBanner>{form.error}</AlertBanner>{/if}
  {#if form?.notice}<AlertBanner variant="warn" role="status">{form.notice}</AlertBanner>{/if}

  <form method="POST" action="?/add" class="trial-add">
    <label for="trial-broadcaster-id">{t('admin.trials.idLabel')}</label>
    <input
      id="trial-broadcaster-id"
      name="broadcaster_id"
      type="text"
      inputmode="numeric"
      pattern="[1-9][0-9]*"
      maxlength="20"
      autocomplete="off"
      required
      bind:value={broadcasterId}
    />
    <Button type="submit" disabled={degraded || activeCount >= 4}>{t('admin.trials.add')}</Button>
  </form>
  <p class="trial-note">{t('admin.trials.note')}</p>

  {#if snapshot === null}
    <SkeletonStack rows={2} height="96px" />
  {:else}
    <p class="trial-count">{t('admin.trials.count', { count: String(activeCount) })}</p>
    {#if active.length === 0}
      <p>{t('admin.trials.empty')}</p>
    {:else}
      <ul class="trial-list">
        {#each [...active].sort((a, b) => a.broadcaster_id.localeCompare(b.broadcaster_id)) as trial (trial.broadcaster_id)}
          <li>
            <div class="trial-main">
              <strong>{trial.display_name?.trim() || trial.broadcaster_id}</strong>
              {#if trial.display_name?.trim()}<span class="trial-id">{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</span>{/if}
              <span class="trial-state" data-state={trial.state}>{t(`admin.trials.state.${trial.state}`)}</span>
              {#if trial.error}<span class="trial-error">{trial.error}</span>{/if}
              <small>
                {t('admin.trials.received', { count: String(trial.received ?? 0) })} ·
                {t('admin.trials.decoded', { count: String(trial.decoded ?? 0) })} ·
                {t('admin.trials.processed', { count: String(trial.processed ?? 0) })} ·
                {t('admin.trials.failed', { count: String(trial.failed ?? 0) })} ·
                {t('admin.trials.retried', { count: String(trial.retried ?? 0) })} ·
                {t('admin.trials.blocked', { count: String(trial.blocked_actions ?? 0) })} ·
                {t('admin.trials.latency', { count: String(trial.average_processing_latency_ms ?? 0) })}
              </small>
            </div>
            <div class="trial-controls">
              <form method="POST" action="?/set_enabled" class="trial-switch">
                <span aria-hidden="true">{trial.enabled ? t('admin.trials.on') : t('admin.trials.off')}</span>
                <input type="hidden" name="broadcaster_id" value={trial.broadcaster_id} />
                <input type="hidden" name="enabled" value={trial.enabled ? 'false' : 'true'} />
                <Switch
                  type="submit"
                  checked={trial.enabled}
                  label={t('admin.trials.toggleLabel', { name: trial.display_name?.trim() || trial.broadcaster_id })}
                  disabled={degraded || trial.state === 'stopping'}
                />
              </form>
              {#if trial.state !== 'stopping'}
                <form method="POST" action="?/remove">
                  <input type="hidden" name="broadcaster_id" value={trial.broadcaster_id} />
                  <Button type="submit" variant="destructive">{t('admin.trials.remove')}</Button>
                </form>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
    {#if history.length > 0}
      <h2>{t('admin.trials.history')}</h2>
      <ul class="trial-list">
        {#each [...history].sort((a, b) => a.broadcaster_id.localeCompare(b.broadcaster_id)) as trial (trial.broadcaster_id)}
          <li>
            <div class="trial-main">
              <strong>{trial.display_name?.trim() || trial.broadcaster_id}</strong>
              {#if trial.display_name?.trim()}<span class="trial-id">{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</span>{/if}
              <span class="trial-state">{t(`admin.trials.state.${trial.state}`)}</span>
              <small>
                {t('admin.trials.received', { count: String(trial.received ?? 0) })} ·
                {t('admin.trials.decoded', { count: String(trial.decoded ?? 0) })} ·
                {t('admin.trials.processed', { count: String(trial.processed ?? 0) })} ·
                {t('admin.trials.blocked', { count: String(trial.blocked_actions ?? 0) })} ·
                {t('admin.trials.latency', { count: String(trial.average_processing_latency_ms ?? 0) })}
              </small>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</section>

<style>
  .trial-add { display: flex; align-items: end; gap: .75rem; flex-wrap: wrap; margin: 1.5rem 0 .5rem; }
  .trial-add label { width: 100%; font-weight: 600; }
  .trial-add input { min-height: 2.75rem; padding: .5rem .75rem; border: 1px solid var(--line, #777); border-radius: .4rem; background: var(--surface, transparent); color: inherit; }
  .trial-note, .trial-count { opacity: .75; }
  .trial-list { list-style: none; margin: 1.25rem 0; padding: 0; display: grid; gap: .75rem; }
  .trial-list li { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 1rem; padding: 1rem; border: 1px solid var(--line, #777); border-radius: .6rem; }
  .trial-main { display: grid; gap: .35rem; }
  .trial-state { text-transform: capitalize; }
  .trial-state[data-state='receiving'] { color: #43865b; }
  .trial-state[data-state='failed'], .trial-error { color: #c84949; }
  .trial-main small { opacity: .75; }
  .trial-id { opacity: .7; font-size: .75rem; }
  .trial-controls, .trial-switch { display: flex; align-items: center; flex-wrap: wrap; gap: .75rem; }
  .trial-switch span { font-size: .875rem; }
</style>
