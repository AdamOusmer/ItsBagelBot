<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { Button, Icon, IconButton, RESPONSE_MAX, Text, TextLink, Textarea, getI18n, moduleDef, tModuleLabel } from '@bagel/kit';
  import { requiredModuleVariables } from '@bagel/kit/variables';
  import type { VariableSurface } from '@bagel/kit/variables';
  import VariablePalette from '$lib/components/variables/VariablePalette.svelte';
  import type { SourceDef } from '$lib/components/commands/fetches/FetchSourcePicker.svelte';

  const i18n = getI18n();

  let {
    value = $bindable(''),
    name = 'response',
    surface,
    placeholder,
    invalid = false,
    describedby,
    required = false,
    maxLines = 1,
    maxlength,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged,
    onblur
  }: {
    value: string;
    name?: string;
    surface: VariableSurface;
    placeholder?: string;
    invalid?: boolean;
    describedby?: string;
    required?: boolean;
    maxLines?: number;
    maxlength?: number;
    fetchDefs?: SourceDef[];
    fetchKeys?: { label: string }[];
    onFetchDefsChanged?: (defs: SourceDef[]) => void;
    onblur?: () => void;
  } = $props();

  const requiredModules = $derived(surface === 'custom' || surface === 'timer' ? requiredModuleVariables(value) : []);
  const moduleLink = (id: string) => moduleDef(id)?.href ?? (moduleDef(id) ? `/modules/${id}` : '/commands');
  const parentModule = (id: string) => { const parent = moduleDef(id)?.parent; return parent ? moduleDef(parent) : undefined; };

  const moduleFlags = $derived(page.data.moduleFlags as Record<string, boolean> | undefined);

  let fields = $state(value.split(/\r\n|\r|\n/));
  let areas = $state<(HTMLTextAreaElement | undefined)[]>([]);
  let focused = $state(0);

  $effect(() => {
    value = fields.join('\n');
  });

  function insert(token: string) {
    const i = Math.min(focused, fields.length - 1);
    const el = areas[i];
    const cur = fields[i] ?? '';
    const start = el?.selectionStart ?? cur.length;
    const end = el?.selectionEnd ?? cur.length;
    fields[i] = cur.slice(0, start) + token + cur.slice(end);
    queueMicrotask(() => {
      el?.focus();
      const pos = start + token.length;
      el?.setSelectionRange(pos, pos);
    });
  }

  function focusField(i: number) {
    queueMicrotask(() => areas[i]?.focus());
  }

  function rememberArea(event: FocusEvent, i: number) {
    if (event.currentTarget instanceof HTMLTextAreaElement) areas[i] = event.currentTarget;
    focused = i;
  }

  function addLine(after: number = fields.length - 1) {
    if (fields.length >= maxLines) return;
    fields.splice(after + 1, 0, '');
    focused = after + 1;
    focusField(focused);
  }

  function removeLine(i: number) {
    fields.splice(i, 1);
    if (fields.length === 0) fields.push('');
    focused = Math.min(i, fields.length - 1);
    focusField(focused);
  }

  function onKeydown(e: KeyboardEvent, i: number) {
    if (e.key === 'Enter') {
      e.preventDefault();
      addLine(i);
    } else if (e.key === 'Backspace' && fields.length > 1 && fields[i] === '') {
      e.preventDefault();
      removeLine(i);
    }
  }

  function onInput(i: number) {
    const v = fields[i];
    if (!/[\r\n]/.test(v)) return;
    const parts = v.split(/\r\n|\r|\n/);
    const room = maxLines - fields.length;
    const keep = parts.slice(0, room + 1);
    const overflow = parts.slice(room + 1);
    if (overflow.length) keep[keep.length - 1] = [keep[keep.length - 1], ...overflow].join(' ');
    fields.splice(i, 1, ...keep);
    focused = Math.min(i + keep.length - 1, fields.length - 1);
  }

  const countTone = (line: string) => (line.length > RESPONSE_MAX ? 'danger' : 'muted');

  const fieldPlaceholder = (i: number) =>
    i === 0 ? (placeholder ?? i18n.t('commandEditor.responsePlaceholder')) : i18n.t('commandEditor.linePlaceholder');
</script>

{#if maxLines > 1}
  <div class="lines" role="group" aria-label={i18n.t('commandEditor.response')}>
    {#each fields as _, i (i)}
      <div class="line-field">
        <span class="line-idx" aria-hidden="true">{i + 1}</span>
        <div class="resp-wrap">
          <Textarea
            rows={2}
            fill
            {invalid}
            placeholder={fieldPlaceholder(i)}
            aria-invalid={invalid ? 'true' : undefined}
            aria-describedby={describedby}
            {required}
            bind:value={fields[i]}
            {maxlength}
            onfocus={(e: FocusEvent) => rememberArea(e, i)}
            onkeydown={(e: KeyboardEvent) => onKeydown(e, i)}
            oninput={() => onInput(i)}
            {onblur}
          />
          <Text as="span" size="xs" mono tone={countTone(fields[i])}>{fields[i].length}/{RESPONSE_MAX}</Text>
        </div>
        {#if fields.length > 1}
          <span class="line-remove">
            <IconButton
              size="sm"
              danger
              title={i18n.t('commandEditor.removeLine', { n: String(i + 1) })}
              label={i18n.t('commandEditor.removeLine', { n: String(i + 1) })}
              onclick={() => removeLine(i)}
            ><Icon name="x" /></IconButton>
          </span>
        {/if}
      </div>
    {/each}
  </div>
  <input type="hidden" {name} {value} />
  <div class="lines-foot">
    <Button variant="add" disabled={fields.length >= maxLines} onclick={() => addLine()}>
      + {i18n.t('commandEditor.addLine')}
    </Button>
    <Text as="small" size="xs" tone="muted">{i18n.t('commandEditor.linesHint', { max: String(maxLines) })}</Text>
  </div>
{:else}
  <div class="resp-wrap">
    <Textarea
      {name}
      rows={4}
      fill
      {invalid}
      placeholder={fieldPlaceholder(0)}
      aria-invalid={invalid ? 'true' : undefined}
      aria-describedby={describedby}
      {required}
      bind:value={fields[0]}
      {maxlength}
      onfocus={(e: FocusEvent) => rememberArea(e, 0)}
      onkeydown={(e: KeyboardEvent) => onKeydown(e, 0)}
      oninput={() => onInput(0)}
      {onblur}
    />
    <Text as="span" size="xs" mono tone={countTone(fields[0])}>{fields[0].length}/{RESPONSE_MAX}</Text>
  </div>
{/if}

<div role="toolbar" aria-label={i18n.t('commandEditor.insertVariable')}>
  <VariablePalette {surface} {moduleFlags} {insert} {fetchDefs} {fetchKeys} {onFetchDefsChanged} />
</div>

{#if requiredModules.length}
  <div class="module-requirements">
    {#each requiredModules as mod (mod.id)}
      <Text size="xs" tone="muted">{i18n.t('commandEditor.moduleVariableRequires', { module: i18n.t(`vars.${mod.id}.name`) })} <TextLink variant="inline" href={moduleLink(mod.head)}>{i18n.t('commandEditor.manageModules')}</TextLink></Text>
      {@const parent = parentModule(mod.head)}
      {#if parent}<Text size="xs" tone="muted">{i18n.t('commandEditor.moduleVariableParentRequires', { module: tModuleLabel(i18n.t, parent) })}</Text>{/if}
    {/each}
  </div>
{/if}

<style>
  .module-requirements { display: flex; flex-direction: column; gap: 6px; margin-top: 6px; }

  .resp-wrap { flex: 1; min-width: 0; display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }

  .lines { display: flex; flex-direction: column; gap: 8px; }
  .line-field { display: flex; align-items: flex-start; gap: 8px; }

  .line-idx {
    flex: none;
    margin-top: 9px;
    width: 18px;
    height: 18px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    border: 1px solid var(--bb-glass-border);
    border-radius: var(--bb-radius-pill);
  }

  .line-remove { flex: none; margin-top: 4px; }

  .lines-foot {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 8px;
    flex-wrap: wrap;
  }
</style>
