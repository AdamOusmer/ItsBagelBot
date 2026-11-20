<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import type { SubmitFunction } from '$app/forms';
  import { onMount, untrack } from 'svelte';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import { getI18n, MOD, moduleDef, type ModuleField } from '@bagel/kit';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Pager from '@bagel/ui/svelte/Pager.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { postAction } from '#lib/client-actions.svelte';
  import ModuleSettingsForm from '#lib/components/modules/ModuleSettingsForm.svelte';
  import LivePanel from '#lib/components/giveaways/LivePanel.svelte';
  import PastWinners from '#lib/components/giveaways/PastWinners.svelte';
  import PresetEditor from '#lib/components/giveaways/PresetEditor.svelte';
  import StartForm from '#lib/components/giveaways/StartForm.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import type { LiveGiveaways } from '#lib/server/channel-giveaways';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();

  const { t } = getI18n();
  const def = moduleDef(MOD.giveaway)!;
  const loadKey = 'giveaways:live';

  let live: LiveGiveaways = $derived(data.live);
  let now = $state(untrack(() => data.serverNow));
  let busy = $state(false);
  let setting = $state('');
  let statuses = $state<Record<string, SaveState>>({});
  let poll: AbortController | undefined;
  const openRound = $derived(live.rounds.some((round) => round.phase === 'open'));
  const blocked = $derived(live.degraded || openRound || data.degraded);
  const availabilityNotice = $derived(live.degraded ? t('giveaways.liveUnavailable') : t('giveaways.settingsUnavailable'));
  const notice = $derived(form?.error || (live.degraded || data.degraded ? availabilityNotice : ''));

  const idle = () => !(busy || setting);
  const pollable = () => idle() && !document.hidden;
  const conflicted = (result: { type: string; status?: number }) => result.type === 'failure' && result.status === 409;
  const refreshAfter = (result: { type: string; status?: number }) => result.type === 'success' || conflicted(result);

  onMount(() => {
    if (data.demo) return;
    const skew = data.serverNow - Date.now();
    const clock = setInterval(() => { now = Date.now() + skew; }, 1000);
    const delay = () => live.rounds.length ? 2000 : 10_000;
    const stop = livePoll(async () => {
      if (pollable()) await pollLive();
      return false;
    }, { firstDelayMs: delay(), delayMs: delay, timeoutMs: Infinity, hiddenDelayMs: 10_000, refreshOnVisible: true });
    return () => {
      stop();
      poll?.abort();
      clearInterval(clock);
    };
  });

  function completed(next: LiveGiveaways): boolean {
    return !next.degraded && live.rounds.some((round) => !next.rounds.some((item) => item.id === round.id));
  }

  async function pollLive() {
    const request = new AbortController();
    poll = request;
    try {
      const response = await fetch('/giveaways/live', { signal: request.signal });
      if (!response.ok) throw new Error('Draw status unavailable');
      const next: LiveGiveaways = await response.json();
      if (request.signal.aborted) return;
      if (completed(next)) {
        await invalidate(loadKey);
        return;
      }
      live = next.degraded ? { ...live, degraded: true } : next;
    } catch {
      if (request.signal.aborted) return;
      live = { ...live, degraded: true };
    }
  }

  const submit: SubmitFunction = ({ cancel }) => {
    if (busy || setting) return cancel();
    busy = true;
    poll?.abort();
    return async ({ result, update }) => {
      try {
        await update({ reset: false, refreshAll: false });
        if (refreshAfter(result)) await invalidate(loadKey);
      } finally {
        busy = false;
      }
    };
  };

  async function save(action: 'toggle' | 'saveSettings', fields: Record<string, string>, nextEnabled = data.enabled) {
    if (busy || setting) return;
    const key = fields.key ? `setting:${fields.key}` : action;
    const body = new FormData();
    body.set('expected_rev', String(data.revision));
    body.set('is_enabled', nextEnabled ? 'on' : 'off');
    for (const [name, value] of Object.entries(fields)) body.set(name, value);
    setting = key;
    statuses[key] = 'saving';
    poll?.abort();
    try {
      const result = await postAction(action, body);
      if (!result?.ok) toast('danger', result?.error ?? t('giveaways.actionFailed'));
      statuses[key] = result?.ok ? 'saved' : 'error';
      await invalidate(loadKey);
    } finally {
      setting = '';
    }
  }

  function saveSetting(field: ModuleField, value: string) {
    if (value !== data.config.fields[field.key]) void save('saveSettings', { key: field.key, value });
  }
</script>

<svelte:head><title>{t('nav.giveaways')}</title></svelte:head>

<div class="pageContent">
  <PageHead><bb-fit><span data-fit="">{t('nav.giveaways')}</span></bb-fit></PageHead>

  <PageNotice tone="warning" visible={!!notice}>{notice}</PageNotice>

  <Card>
    <div class="section">
      <CardHead title={t('giveaways.new')} headingLevel={2} />
      <StartForm config={data.config} enabled={data.enabled} {blocked} busy={busy || !!setting} demo={data.demo} {submit} />
    </div>
  </Card>

  {#each live.rounds as round (round.id)}
    <LivePanel {round} {now} busy={busy || !!setting || live.degraded} {submit} />
  {/each}

  <PresetEditor config={data.config} revision={data.revision} enabled={data.enabled} {busy} locked={data.degraded || !!setting} {submit} />

  <Card flush>
    <div class="settingsHead">
      <CardHead title={t('giveaways.settings')} headingLevel={2} />
      <SwitchRow control="end" fitCopy label={t('giveaways.enabled')} checked={data.enabled}
        disabled={data.degraded || busy || !!setting} busy={setting === 'toggle'} onCheckedChange={(checked) => save('toggle', {}, checked)} />
    </div>
    <ModuleSettingsForm {def} config={data.config.fields} modStatus={statuses} locked={data.degraded || busy || !!setting} {saveSetting} />
  </Card>

  <section id="giveaway-history" aria-label={t('giveaways.history')}>
    <PastWinners winners={data.history.winners} degraded={data.history.degraded} busy={busy || !!setting} {submit}>
      {#snippet pager()}
        {#if data.history.page > 1 || data.history.more}
          <Pager fixed fit label={t('giveaways.historyPage', { page: data.history.page })}
            hasPrev={data.history.page > 1} hasNext={data.history.more}
            prevHref={`?winners=${data.history.page - 1}#giveaway-history`}
            nextHref={`?winners=${data.history.page + 1}#giveaway-history`} />
        {/if}
      {/snippet}
    </PastWinners>
  </section>
</div>

<style>
  .pageContent { display: grid; min-width: 0; gap: var(--bb-space-5); }
  .pageContent, .section, .settingsHead { grid-template-columns: minmax(0, 1fr); }
  .section, .settingsHead { display: grid; gap: var(--bb-space-3); min-width: 0; }
  .settingsHead { padding: var(--bb-space-4) var(--bb-space-5); }
</style>
