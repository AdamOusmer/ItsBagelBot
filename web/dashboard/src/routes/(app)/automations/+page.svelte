<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { deserialize, type SubmitFunction } from '$app/forms';
  import { goto, invalidateAll } from '$app/navigation';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, getI18n, MasterToggle, type ActionOk, type MessageKey } from '@bagel/kit';
  import type { Automation } from '@bagel/kit/automations';
  import AutomationRow from '#lib/components/automations/AutomationRow.svelte';

  type Payload = ActionOk & { code?: string };

  let { data } = $props();
  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let automations = $state<Automation[]>(data.automations ?? []);
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data === seed) return;
    seed = data;
    automations = data.automations ?? [];
    enabled = data.enabled ?? false;
  });

  const usable = $derived(enabled && !data.degraded);
  const rows = $derived(automations.toSorted((a, b) => a.name.localeCompare(b.name)));
  let pendingDelete = $state<Automation | null>(null);

  const failure = (payload: Payload | undefined, fallback: MessageKey) =>
    toast('danger', t(payload?.code ? (`automations.err.${payload.code}` as MessageKey) : fallback));

  async function post(action: string, fields: Record<string, string>): Promise<Payload | undefined> {
    const body = new FormData();
    for (const [key, value] of Object.entries(fields)) body.set(key, value);
    return fetch(`?/${action}`, { method: 'POST', body })
      .then(async (res) => actionPayload<Payload>(deserialize(await res.text())))
      .catch(() => undefined);
  }

  const toggleSubmit =
    (item: Automation): SubmitFunction =>
    () => {
      const was = item.enabled;
      automations = automations.map((a) => (a.id === item.id ? { ...a, enabled: !was } : a));
      return async ({ result }) => {
        const payload = actionPayload<Payload>(result);
        if (result.type === 'success' && payload?.ok) {
          await invalidateAll();
          return;
        }
        automations = automations.map((a) => (a.id === item.id ? { ...a, enabled: was } : a));
        failure(payload, 'automations.toastToggleFailed');
        if (payload?.code === 'conflict') await invalidateAll();
      };
    };

  async function runNow(item: Automation) {
    const res = await post('run', { name: item.name });
    if (res?.ok) toast('success', t('automations.toastRunQueued'));
    else failure(res, 'automations.err.failed');
  }

  async function confirmDelete() {
    const item = pendingDelete;
    pendingDelete = null;
    if (!item) return;
    const res = await post('remove', { id: item.id });
    if (res?.ok) {
      toast('success', t('automations.toastDeleted'));
      await invalidateAll();
    } else failure(res, 'automations.toastDeleteFailed');
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('automations.eyebrow')} description={t('automations.description')}>
    {t('automations.titlePre')}<em>{t('automations.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('automations.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet leading()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('automations.botOn')}
        hint={t('automations.botOnHint')}
        ariaLabel={t('automations.botOn')}
        failMessage={t('automations.toastToggleFailed')}
      />
    {/snippet}
    {#snippet trailing()}
      <Button variant="primary" disabled={!usable} onclick={() => goto('/automations/new')}>{t('automations.newAutomation')}</Button>
    {/snippet}
  </PageToolbar>

  <DeckList>
    <div>
      {#each rows as item (item.id)}
        <AutomationRow
          automation={item}
          {usable}
          toggleSubmit={toggleSubmit(item)}
          onRun={() => runNow(item)}
          onDelete={() => (pendingDelete = item)}
        />
      {/each}
      {#if rows.length === 0}
        <EmptyState title={t('automations.emptyTitle')} body={enabled ? t('automations.emptySub') : t('automations.emptyOff')}>
          <Button variant="primary" disabled={!usable} onclick={() => goto('/automations/new')}>{t('automations.newAutomation')}</Button>
        </EmptyState>
      {/if}
    </div>
  </DeckList>
</section>

<ConfirmDialog
  open={pendingDelete !== null}
  title={t('automations.deleteTitle')}
  body={t('automations.deleteBody', { name: pendingDelete?.name ?? '' })}
  confirmLabel={t('automations.delete')}
  cancelLabel={t('common.cancel')}
  onCancel={() => (pendingDelete = null)}
  onConfirm={confirmDelete}
  tone="danger"
/>
