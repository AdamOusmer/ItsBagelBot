<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import { overrideValue, withOverride, type OverrideGroup, type Variation } from '../../lib/overlay-editor/alert-rules';
  import type { MediaBinding } from '../../lib/overlay-editor/binding';
  import type { Settings } from '../../lib/overlay-editor/defaults';
  import { labelOf, typography, variationFields, variationLook, type SettingField } from '../../lib/overlay-editor/fields';
  import Fit from './Fit.svelte';
  import SettingControl from './SettingControl.svelte';

  let { item, entry, style, change, remove, binding }: {
    item: Variation;
    entry: Settings;
    style: Settings;
    change: (item: Variation) => void;
    remove: () => void;
    binding?: MediaBinding;
  } = $props();

  const { t } = getI18n();
  const groups: { group: OverrideGroup; fields: SettingField[] }[] = [
    { group: 'look', fields: variationLook },
    { group: 'style', fields: typography }
  ];

  const inherited = (group: OverrideGroup, key: string): unknown => (group === 'style' ? style[key] : entry[key]);
  const fieldName = (field: SettingField): string => t(`overlayEditor.field.${labelOf(field)}`).toLowerCase();
  const isPool = (field: SettingField): boolean => item.condition === 'random' && field.key === 'value';
</script>

<Stack gap={3}>
  {#each variationFields as field (field.key)}
    <SettingControl {field} value={(item as Settings)[field.key]} disabled={isPool(field)} update={(next) => change({ ...item, [field.key]: next })} />
  {/each}
  {#each groups as { group, fields } (group)}
    {#each fields as field (field.key)}
      {@const existing = overrideValue(item, group, field.key)}
      {#if existing === undefined}
        <Button size="sm" variant="secondary" onclick={() => change(withOverride(item, group, field.key, inherited(group, field.key)))}>
          <Fit>{t('overlayEditor.ui.override', { field: fieldName(field) })}</Fit>
        </Button>
      {:else}
        <SettingControl {field} value={existing} {binding} update={(next) => change(withOverride(item, group, field.key, next))} />
        <Button size="sm" variant="secondary" onclick={() => change(withOverride(item, group, field.key, undefined))}>
          <Fit>{t('overlayEditor.ui.useDefault', { field: fieldName(field) })}</Fit>
        </Button>
      {/if}
    {/each}
  {/each}
  <Button size="sm" variant="secondary" onclick={remove}>
    <Fit>{t('overlayEditor.ui.removeVariation')}</Fit>
  </Button>
</Stack>
