<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/collapse.css';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { ImportFailure, type ImportReport } from '@bagel/kit/overlay-editor';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import FileDrop from '@bagel/ui/svelte/FileDrop.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import { importArchive } from '#lib/overlay-import';
  import ImportReportView from './ImportReportView.svelte';

  let { premium, onimported }: { premium: boolean; onimported: (id: number) => void } = $props();

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
    if (!chosen || !premium) return Promise.resolve();
    return run(async () => {
      const result = await importArchive(chosen, false);
      if (result.overlay_id) onimported(result.overlay_id);
    });
  }
</script>

<Disclosure summary={t('overlayEditor.import.title')} size="sm">
  <Stack gap={3}>
    <Text size="sm" tone="muted">{t('overlayEditor.import.hint')}</Text>
    <FileDrop label={t('overlayEditor.import.choose')} accept=".zip,application/zip" {file} disabled={busy} onFileChange={analyse} />
    <PageNotice tone="danger" role="alert" visible={!!error}>{error}</PageNotice>
    <div class="bb-collapse" class:is-open={report !== null} inert={report === null} aria-hidden={report === null}>
      <div>
        <Stack gap={3}>
          {#if report}<ImportReportView {report} />{/if}
          <Cluster gap={3}>
            <Button size="sm" {busy} disabled={!premium || !importable} onclick={confirm}>{t('overlayEditor.import.confirm')}</Button>
            <Text size="xs" tone="muted">{premium ? t('overlayEditor.import.confirmHint') : t('overlayEditor.import.premiumNote')}</Text>
          </Cluster>
        </Stack>
      </div>
    </div>
  </Stack>
</Disclosure>
