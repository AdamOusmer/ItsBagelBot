<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { postAction } from '#lib/client-actions.svelte';
  import { flowKey, type FlowExportReply, type ShareableFlow } from '#lib/flow-share';

  let { flows, path = '', startOpen = false }: { flows: ShareableFlow[]; path?: string; startOpen?: boolean } = $props();

  const { t } = getI18n();

  let open = $state(untrack(() => startOpen));
  let busy = $state(false);
  let selected = $state(new Set<string>());
  let summary = $state<FlowExportReply | null>(null);

  const rows = $derived(flows.map((flow) => ({ key: flowKey(flow.ref), name: flow.name })));
  const picked = $derived(rows.filter((row) => selected.has(row.key)));

  function toggle(key: string, on: boolean) {
    const next = new Set(selected);
    if (on) next.add(key);
    else next.delete(key);
    selected = next;
  }

  function saveFile(text: string) {
    const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = `itsbagelbot-recipes-${new Date().toISOString().slice(0, 10)}.json`;
    link.click();
    URL.revokeObjectURL(url);
  }

  async function run() {
    if (picked.length === 0 || busy) return;
    busy = true;
    const body = new FormData();
    for (const row of picked) body.append('flow', row.key);
    const reply = await postAction<FlowExportReply>('flowExport', body, path);
    busy = false;
    if (!reply?.ok || !reply.file) {
      toast('danger', reply?.error ?? t('automations.share.exportError'));
      return;
    }
    saveFile(reply.file);
    summary = reply;
  }

  function close() {
    open = false;
    selected = new Set();
    summary = null;
  }
</script>

<Button variant="secondary" onclick={() => (open = true)}>{t('automations.share.exportOpen')}</Button>

<Modal bind:open title={t('automations.share.exportTitle')} onClose={close} {busy}>
  <div class="body">
    {#if summary}
      <p>{t('automations.share.exportDone', { n: summary.exported ?? 0 })}</p>
      {#if summary.needSetup}<p>{t('automations.share.exportNeedSetup', { n: summary.needSetup })}</p>{/if}
      {#if summary.missing}<p>{t('automations.share.exportMissing', { n: summary.missing })}</p>{/if}
      {#if summary.failed?.length}<p>{t('automations.share.exportFailed', { names: summary.failed.join(', ') })}</p>{/if}
      <div class="foot">
        <Button onclick={close}>{t('automations.share.importClose')}</Button>
      </div>
    {:else}
      <p>{t('automations.share.exportBody')}</p>
      <div class="tools">
        <span class="count">{t('automations.share.exportSelected', { n: picked.length })}</span>
        <Button variant="ghost" size="sm" onclick={() => (selected = new Set(rows.map((row) => row.key)))}>{t('automations.share.exportSelectAll')}</Button>
        <Button variant="ghost" size="sm" onclick={() => (selected = new Set())}>{t('automations.share.exportSelectNone')}</Button>
      </div>
      <ul class="list">
        {#each rows as row (row.key)}
          <li>
            <Checkbox checked={selected.has(row.key)} onchange={(event) => toggle(row.key, event.currentTarget.checked)}>{row.name}</Checkbox>
          </li>
        {/each}
      </ul>
      <div class="foot">
        <Button variant="ghost" disabled={busy} onclick={close}>{t('common.cancel')}</Button>
        <Button {busy} disabled={picked.length === 0} onclick={run}>{t('automations.share.exportCta')}</Button>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  p {
    margin: 0;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .count {
    flex: 1;
    min-width: 96px;
    font-variant-numeric: tabular-nums;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: min(320px, 40vh);
    overflow-y: auto;
  }
  .foot {
    display: flex;
    justify-content: flex-end;
  }
</style>
