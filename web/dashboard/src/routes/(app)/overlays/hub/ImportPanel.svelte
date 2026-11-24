<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { ImportFailure, type ImportReport } from '@bagel/kit/overlay-editor';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import FileDrop from '@bagel/ui/svelte/FileDrop.svelte';
  import SwapStack from '@bagel/ui/svelte/SwapStack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { importArchive } from '#lib/overlay-import';
  import ImportReportView from './ImportReportView.svelte';

  let { onimported }: { onimported: (id: number) => void } = $props();

  const { t } = getI18n();
  let file = $state<File | null>(null);
  let report = $state<ImportReport | null>(null);
  let busy = $state(false);
  let error = $state('');

  const importable = $derived(report !== null && report.mapped.length + report.approximated.length > 0);

  async function run(work: () => Promise<void>): Promise<void> {
    busy = true;
    error = '';
    try {
      await work();
    } catch (failure) {
      error = t(`overlayEditor.importError.${failure instanceof ImportFailure ? failure.code : 'failed'}`);
    } finally {
      busy = false;
    }
  }

  function analyse(chosen: File | undefined): Promise<void> {
    if (!chosen) return Promise.resolve();
    file = chosen;
    report = null;
    return run(async () => {
      report = (await importArchive(chosen, true)).report;
    });
  }

  function confirm(): Promise<void> {
    const chosen = file;
    if (!chosen) return Promise.resolve();
    return run(async () => {
      const result = await importArchive(chosen, false);
      if (result.overlay_id) onimported(result.overlay_id);
    });
  }
</script>

<div class="import">
  <div class="lead bb-fit-box">
    <Text as="p" size="sm" tone="muted"><bb-fit mode="block">{t('overlayEditor.import.hint')}</bb-fit></Text>
  </div>
  <FileDrop label={t('overlayEditor.import.choose')} accept=".zip,application/zip" {file} disabled={busy} onFileChange={analyse} />
  <div class="report" class:shown={report !== null} inert={report === null} aria-hidden={report === null}>
    {#if report}<ImportReportView {report} />{/if}
  </div>
  <div class="foot">
    <SwapStack class="foot-copy" alternate={!!error} aria-live="polite">
      {#snippet primary()}
        <Text as="div" size="xs" tone="muted">
          <bb-fit mode="block">{t('overlayEditor.import.confirmHint')}</bb-fit>
        </Text>
      {/snippet}
      {#snippet secondary()}
        <Text as="div" size="xs" tone="danger"><bb-fit mode="block">{error}</bb-fit></Text>
      {/snippet}
    </SwapStack>
    <Button size="sm" {busy} disabled={!importable} onclick={confirm}>
      <bb-fit class="confirm-label"><span data-fit="">{t('overlayEditor.import.confirm')}</span></bb-fit>
    </Button>
  </div>
</div>

<style>
  .import {
    display: grid;
    grid-template-rows: 2.5rem auto minmax(0, 1fr) 3.5rem;
    gap: var(--bb-space-3);
    block-size: 100%;
    min-block-size: 0;
  }
  .lead { min-inline-size: 0; align-content: center; }
  .report { min-block-size: 0; overflow: auto; opacity: 0; }
  .report.shown { opacity: 1; }
  .foot {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 9rem;
    align-items: center;
    gap: var(--bb-space-3);
  }
  .foot :global(.foot-copy) { block-size: 3.5rem; }
  .confirm-label { line-height: 1.4; }
  @media (prefers-reduced-motion: no-preference) {
    .report { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
