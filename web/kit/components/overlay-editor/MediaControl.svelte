<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import FileDrop from '@bagel/ui/svelte/FileDrop.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { MediaBinding } from '../../lib/overlay-editor/binding';
  import { formatBytes, refList } from '../../lib/overlay-editor/control-values';
  import { refAssetId } from '../../lib/overlay-editor/document';
  import { labelOf, type SettingField } from '../../lib/overlay-editor/fields';
  import { MediaFailure, slotAccept, uploadError } from '../../lib/overlay-editor/media';
  import Fit from './Fit.svelte';
  import Notice from './Notice.svelte';

  let { field, value, update, disabled = false, binding }: {
    field: SettingField;
    value: unknown;
    update: (value: unknown) => void;
    disabled?: boolean;
    binding?: MediaBinding;
  } = $props();

  const { t } = getI18n();
  let busy = $state(false);
  let error = $state('');

  const slot = $derived(field.slot ?? 'image');
  const multiple = $derived(field.type === 'media-list');
  const refs = $derived(refList(value, multiple));
  const locked = $derived(disabled || busy || !binding);

  function summary(ref: string): string {
    const asset = binding?.assets[refAssetId(ref) ?? ''];
    if (!asset) return t('overlayEditor.ui.fileSelected');
    return t('overlayEditor.ui.assetSummary', { type: asset.type.toUpperCase(), size: formatBytes(asset.bytes) });
  }

  async function choose(file: File | undefined): Promise<void> {
    if (!file || !binding) return;
    busy = true;
    error = '';
    try {
      const ref = await binding.upload(file, slot);
      update(multiple ? [...refs, ref] : ref);
    } catch (failure) {
      const code = uploadError(failure instanceof MediaFailure ? failure.code : 'failed');
      error = t(`overlayEditor.uploadError.${code}`);
    } finally {
      busy = false;
    }
  }

  function drop(index: number): void {
    update(multiple ? refs.filter((_, position) => position !== index) : '');
  }
</script>

<Stack gap={2}>
  <Label as="span"><Fit>{t(`overlayEditor.field.${labelOf(field)}`)}</Fit></Label>
  <FileDrop
    label={binding ? t('overlayEditor.ui.chooseFile') : t('overlayEditor.ui.previewUpload')}
    accept={slotAccept(slot)}
    disabled={locked}
    onFileChange={choose}
  />
  {#each refs as ref, index (`${ref}:${index}`)}
    <Cluster justify="between">
      <Text size="xs" tone="muted">{summary(ref)}</Text>
      <Button size="sm" variant="secondary" {disabled} onclick={() => drop(index)}>
        <Fit>{t('overlayEditor.ui.remove')}</Fit>
      </Button>
    </Cluster>
  {/each}
  <Notice visible={!!error} tone="danger" role="alert">{error}</Notice>
</Stack>
