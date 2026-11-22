<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { MediaBinding } from '../../lib/overlay-editor/binding';
  import { boundedNumber, dateToInput, inputToDate, stringList, textLines } from '../../lib/overlay-editor/control-values';
  import { labelOf, type SettingField } from '../../lib/overlay-editor/fields';
  import MediaControl from './MediaControl.svelte';

  let { field, value, update, disabled = false, binding }: {
    field: SettingField;
    value: unknown;
    update: (value: unknown) => void;
    disabled?: boolean;
    binding?: MediaBinding;
  } = $props();

  const { t } = getI18n();
  const id = $props.id();
  const label = $derived(t(`overlayEditor.field.${labelOf(field)}`));
  const text = $derived(typeof value === 'string' ? value : '');
  const lines = $derived(stringList(value));
  const options = $derived(
    (field.options ?? []).map((option) => ({ value: option, label: field.raw ? option : t(`overlayEditor.option.${option}`) }))
  );

  function changeNumber(next: number): void {
    const bounded = boundedNumber(field, next);
    if (bounded !== undefined) update(bounded);
  }
</script>

{#if field.type === 'boolean'}
  <SwitchRow {label} fitCopy checked={value === true} {disabled} onCheckedChange={update} />
{:else if field.type === 'media' || field.type === 'media-list'}
  <MediaControl {field} {value} {update} {disabled} {binding} />
{:else if field.type === 'color'}
  <Field {label} fit>
    <ColorPicker {id} {label} value={text} {disabled} onValueChange={update} />
  </Field>
{:else}
  <Field {label} fit for={id}>
    {#if field.type === 'select'}
      <Select {id} {label} value={text} {options} {disabled} fill onchange={(event) => update(event.currentTarget.value)} />
    {:else if field.type === 'area'}
      <Textarea {id} value={text} {disabled} rows={3} fill onchange={(event) => update(event.currentTarget.value)} />
    {:else if field.type === 'list'}
      <Textarea {id} value={lines.join('\n')} {disabled} rows={3} fill onchange={(event) => update(textLines(event.currentTarget.value))} />
    {:else if field.type === 'number'}
      <Input
        {id}
        type="number"
        value={typeof value === 'number' ? value : 0}
        min={field.min}
        max={field.max}
        step={field.step ?? 'any'}
        {disabled}
        fill
        onchange={(event) => changeNumber(event.currentTarget.valueAsNumber)}
      />
    {:else if field.type === 'datetime'}
      <Input {id} type="datetime-local" value={dateToInput(text)} {disabled} fill onchange={(event) => update(inputToDate(event.currentTarget.value))} />
    {:else}
      <Input {id} value={text} {disabled} fill onchange={(event) => update(event.currentTarget.value)} />
    {/if}
  </Field>
{/if}
