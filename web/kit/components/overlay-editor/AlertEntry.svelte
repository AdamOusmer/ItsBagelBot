<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import {
    asSettings, bucketOf, entryOf, newVariation, patchEntry, replaceVariation, variationsOf, type Variation
  } from '../../lib/overlay-editor/alert-rules';
  import type { MediaBinding } from '../../lib/overlay-editor/binding';
  import type { AlertKind, Settings } from '../../lib/overlay-editor/defaults';
  import { alertFields, listRule, typography } from '../../lib/overlay-editor/fields';
  import Fit from './Fit.svelte';
  import SettingControl from './SettingControl.svelte';
  import VariationEditor from './VariationEditor.svelte';

  let { kind, event, settings, update, binding }: {
    kind: 'alertbox' | 'eventlist';
    event: AlertKind;
    settings: Settings;
    update: (settings: Settings) => void;
    binding?: MediaBinding;
  } = $props();

  const { t } = getI18n();
  const bucket = $derived(bucketOf(kind));
  const entry = $derived(entryOf(settings, bucket, event));
  const entryFields = $derived((kind === 'alertbox' ? alertFields : listRule).filter((field) => field.key !== 'enabled'));
  const style = $derived(asSettings(entry.style));
  const variations = $derived(variationsOf(entry));

  const change = (patch: Settings): void => update(patchEntry(settings, bucket, event, patch));
  const setVariation = (index: number, item: Variation): void => change({ variations: replaceVariation(variations, index, item) });
  const conditionName = (item: Variation): string => t(`overlayEditor.option.${item.condition}`);
</script>

<Stack gap={3}>
  {#each entryFields as field (field.key)}
    <SettingControl {field} value={entry[field.key]} update={(next) => change({ [field.key]: next })} {binding} />
  {/each}
  {#if kind === 'alertbox'}
    <Disclosure summary={t('overlayEditor.ui.alertTypography')} size="sm">
      <Stack gap={3}>
        {#each typography as field (field.key)}
          <SettingControl {field} value={style[field.key]} update={(next) => change({ style: { ...style, [field.key]: next } })} />
        {/each}
      </Stack>
    </Disclosure>
    <Disclosure summary={t('overlayEditor.ui.variations')} size="sm">
      <Stack gap={3}>
        {#each variations as item, index (index)}
          <Disclosure summary={t('overlayEditor.ui.variationItem', { number: index + 1, condition: conditionName(item) })} size="sm">
            <VariationEditor
              {item}
              {entry}
              {style}
              {binding}
              change={(next) => setVariation(index, next)}
              remove={() => change({ variations: variations.filter((_, position) => position !== index) })}
            />
          </Disclosure>
        {/each}
        <Text size="xs" tone="muted">{t('overlayEditor.ui.variationHint')}</Text>
        <Button size="sm" variant="secondary" onclick={() => change({ variations: [...variations, newVariation()] })}>
          <Fit>{t('overlayEditor.ui.addVariation')}</Fit>
        </Button>
      </Stack>
    </Disclosure>
  {/if}
</Stack>
