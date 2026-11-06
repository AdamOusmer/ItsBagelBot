<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import type { ShareableFlow } from '#lib/flow-share';

  let { flows, onImported }: { flows: ShareableFlow[]; onImported: () => void } = $props();

  const { t } = getI18n();

  let exporting = $state(false);
  let importing = $state(false);

  const loadExport = () => import('#lib/components/flow-share/FlowExportControl.svelte');
  const loadImport = () => import('#lib/components/flow-share/FlowImportDialog.svelte');
</script>

{#if exporting}
  {#await loadExport() then { default: ExportControl }}
    <ExportControl {flows} startOpen />
  {/await}
{:else}
  <Button variant="secondary" onpointerenter={loadExport} onclick={() => (exporting = true)}>{t('automations.share.exportOpen')}</Button>
{/if}

{#if importing}
  {#await loadImport() then { default: ImportDialog }}
    <ImportDialog {onImported} startOpen />
  {/await}
{:else}
  <Button variant="secondary" onpointerenter={loadImport} onclick={() => (importing = true)}>{t('automations.share.importOpen')}</Button>
{/if}
