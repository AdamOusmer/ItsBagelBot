<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, getI18n, type MessageKey } from '@bagel/kit';
  import AutomationEditor from '#lib/components/automations/AutomationEditor.svelte';
  import RunsPanel from '#lib/components/automations/RunsPanel.svelte';
  import { MANUAL_RUN_TRIGGERS } from '#lib/components/automations/model';

  let { data } = $props();
  const { t } = getI18n();

  const automation = $derived(data.automation ?? null);
  const creating = $derived(data.id === 'new');
  const usable = $derived(data.enabled === true && !data.degraded);
  const runnable = $derived(
    !!automation && automation.enabled && usable && MANUAL_RUN_TRIGGERS.includes(automation.definition.trigger.type)
  );
  let running = $state(false);
  let refreshing = $state(false);

  async function runNow() {
    if (!automation) return;
    running = true;
    const body = new FormData();
    body.set('name', automation.name);
    const res = await fetch('?/run', { method: 'POST', body })
      .then(async (r) => actionPayload<{ ok?: boolean; code?: string }>(deserialize(await r.text())))
      .catch(() => undefined);
    running = false;
    if (res?.ok) toast('success', t('automations.toastRunQueued'));
    else toast('danger', t(`automations.err.${res?.code ?? 'failed'}` as MessageKey));
    setTimeout(refresh, 1200);
  }

  async function refresh() {
    refreshing = true;
    await invalidateAll();
    refreshing = false;
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('automations.eyebrow')} description={t('automations.editorDescription')}>
    {creating ? t('automations.newAutomation') : (automation?.name ?? t('automations.titlePre'))}
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('automations.degraded')}</AlertBanner>
  {:else if data.missing}
    <AlertBanner>{t('automations.notFound')} <TextLink variant="inline" href="/automations">{t('automations.backToList')}</TextLink></AlertBanner>
  {:else if !data.enabled}
    <AlertBanner>{t('automations.moduleOff')} <TextLink variant="inline" href="/automations">{t('automations.enableOnList')}</TextLink></AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet leading()}
      <TextLink href="/automations">{t('automations.backToList')}</TextLink>
    {/snippet}
    {#snippet trailing()}
      <Button variant="secondary" busy={running} disabled={!runnable} onclick={runNow}>{t('automations.runNow')}</Button>
    {/snippet}
  </PageToolbar>

  {#if !data.missing && !data.degraded}
    {#key data.id}
      <AutomationEditor {automation} others={data.others ?? []} {usable} />
    {/key}
    {#if !creating}
      <RunsPanel runs={data.runs ?? []} busy={refreshing} onRefresh={refresh} />
    {/if}
  {/if}
</section>
