<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit';
  import type { RunRecord } from '@bagel/kit/automations';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import { postAction } from '#lib/client-actions.svelte';
  import RunsPanel from '#lib/components/automations/RunsPanel.svelte';
  import { flowRouteId, type FlowRef } from '#lib/flow/ref';
  import type { Flow } from '#lib/flow/model';
  import Icon from './FlowIcon.svelte';

  let { flow = $bindable(), ref, onclose }: { flow: Flow; ref: FlowRef | null; onclose: () => void } = $props();

  const { t } = getI18n();
  const uid = $props.id();

  let runs = $state<RunRecord[]>([]);
  let busy = $state(false);
  let failed = $state(false);

  const hasRuns = $derived(ref?.layer === 'automation');

  async function load() {
    if (!ref || ref.layer !== 'automation') return;
    busy = true;
    const body = new FormData();
    body.set('id', flowRouteId(ref));
    const reply = await postAction<{ ok: boolean; runs?: RunRecord[] }>('runs', body);
    busy = false;
    failed = !reply?.ok;
    if (reply?.ok) runs = reply.runs ?? [];
  }

  const routeId = $derived(ref ? flowRouteId(ref) : null);

  $effect(() => {
    if (routeId) untrack(() => void load());
  });
</script>

<aside class="panel" aria-label={t('flows.recipe.title')} tabindex="-1">
  <header class="head">
    <h2 class="name">{t('flows.recipe.title')}</h2>
    <IconButton label={t('flows.recipe.close')} size="sm" onclick={onclose}><Icon name="x" size={14} /></IconButton>
  </header>
  <div class="scroll">
    <section class="group">
      <h3 class="group-title">{t('flows.recipe.queueTitle')}</h3>
      <Field label={t('flows.recipe.queueLabel')} for="{uid}-queue">
        <Input
          id="{uid}-queue"
          maxlength={32}
          placeholder={t('flows.recipe.queuePlaceholder')}
          value={flow.queue ?? ''}
          oninput={(e) => {
            const next = e.currentTarget.value;
            if (next) flow.queue = next;
            else delete flow.queue;
          }}
        />
      </Field>
      <p class="note">{t('flows.recipe.queueHint')}</p>
    </section>
    <section class="group">
      {#if hasRuns}
        {#if failed}<p class="note">{t('flows.recipe.runsFailed')}</p>{/if}
        <RunsPanel {runs} {busy} {flow} title={t('flows.recipe.runsTitle')} empty={t('flows.recipe.runsEmpty')} onRefresh={() => void load()} />
      {:else}
        <h3 class="group-title">{t('flows.recipe.runsTitle')}</h3>
        <p class="note">{t('flows.recipe.runsSaveFirst')}</p>
      {/if}
    </section>
  </div>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    border-radius: 12px;
    background: var(--bb-bg-1);
    box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex: none;
    padding: 16px 12px 8px 16px;
  }
  .name {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 17px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 28px;
    padding: 12px 16px 20px;
    --field-mb: 0;
    --field-gap: 8px;
  }
  .scroll > :global(*) {
    flex: none;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .group-title {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .note {
    margin: 0;
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
</style>
