<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import type { Condition, Trigger, TypeDef } from '@bagel/kit/automations';
  import type { VariableSurface } from '@bagel/kit/variables';
  import TypeFields from './TypeFields.svelte';
  import { issueAt, type IssueIndex } from './model';

  let {
    node = $bindable(),
    defs,
    make,
    path,
    issues,
    surface,
    removeLabel,
    onRemove
  }: {
    node: Trigger | Condition;
    defs: TypeDef[];
    make: (type: string) => Trigger | Condition;
    path: string;
    issues: IssueIndex;
    surface: VariableSurface;
    removeLabel?: string;
    onRemove?: () => void;
  } = $props();

  const { t } = getI18n();

  const options = $derived(defs.map((d) => ({ value: d.type, label: t(`automations.${d.group}.${d.type}` as MessageKey) })));
  const def = $derived(defs.find((d) => d.type === node.type));
  const typeCode = $derived(issueAt(issues, `${path}.type`));

  function changeType(type: string) {
    node.type = type;
    node.params = make(type).params;
  }

  function onFieldChange(name: string) {
    if (node.type === 'event' && name === 'kind' && node.params) node.params.filters = {};
  }
</script>

<div class="tn">
  <div class="tn-head">
    <div class="tn-type">
      <Select fill label={t('automations.fieldType')} {options} invalid={!!typeCode} value={node.type} onchange={(e) => changeType(e.currentTarget.value)} />
    </div>
    <span class="tn-err"><Text as="span" size="xs" tone="danger">{typeCode ? t(`automations.issue.${typeCode}` as MessageKey) : ''}</Text></span>
    {#if onRemove}
      <IconButton size="sm" tone="danger" label={removeLabel ?? t('automations.remove')} onclick={onRemove}><Icon name="trash" size={15} /></IconButton>
    {/if}
  </div>
  {#if def && node.params}
    <TypeFields {def} bind:params={node.params} {path} {issues} {surface} {onFieldChange} />
  {/if}
</div>

<style>
  .tn { display: flex; flex-direction: column; gap: 8px; }
  .tn-head { display: grid; grid-template-columns: 280px minmax(0, 1fr) 32px; align-items: center; column-gap: 12px; min-height: 40px; }
  .tn-type { min-width: 0; }
  .tn-err { min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
</style>
