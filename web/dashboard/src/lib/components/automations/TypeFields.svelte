<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n, type MessageKey } from '@bagel/kit';
  import type { Params, TypeDef } from '@bagel/kit/automations';
  import type { VariableSurface } from '@bagel/kit/variables';
  import ParamField from './ParamField.svelte';
  import { eventFieldNames, type IssueIndex } from './model';

  const STRUCTURAL = new Set(['actions', 'condition', 'branches']);
  const CHAT_LINES = 3;

  let {
    def,
    params = $bindable(),
    path,
    issues,
    surface,
    runTargets,
    disabled = false,
    onFieldChange
  }: {
    def: TypeDef;
    params: Params;
    path: string;
    issues: IssueIndex;
    surface: VariableSurface;
    runTargets?: { value: string; label: string }[];
    disabled?: boolean;
    onFieldChange?: (name: string) => void;
  } = $props();

  const { t } = getI18n();

  const fields = $derived(def.fields.filter((f) => !STRUCTURAL.has(f.kind)));
  const kindFields = $derived(typeof params.kind === 'string' ? eventFieldNames(params.kind) : []);

  function choicesFor(name: string) {
    return def.type === 'run' && name === 'automation_id' ? runTargets : undefined;
  }

  function linesFor(name: string): number {
    return def.type === 'chat' && name === 'text' ? CHAT_LINES : 1;
  }
</script>

<div class="tf" data-type={def.type} aria-label={t(`automations.${def.group}.${def.type}` as MessageKey)}>
  {#each fields as field (field.name)}
    <ParamField
      {field}
      bind:params
      path={`${path}.${field.name}`}
      {issues}
      {surface}
      {kindFields}
      {disabled}
      choices={choicesFor(field.name)}
      maxLines={linesFor(field.name)}
      onchange={onFieldChange}
    />
  {/each}
</div>

<style>
  .tf { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; align-items: start; }
</style>
