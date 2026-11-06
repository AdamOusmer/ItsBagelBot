<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import type { FieldDef, Params } from '@bagel/kit/automations';
  import type { VariableSurface } from '@bagel/kit/variables';
  import ResponseEditor from '#lib/components/commands/ResponseEditor.svelte';
  import { isNumericEnum, issueAt, type IssueIndex } from './model';

  interface Choice {
    value: string;
    label: string;
  }

  let {
    field,
    params = $bindable(),
    path,
    issues,
    surface,
    kindFields = [],
    choices,
    maxLines = 1,
    disabled = false,
    onchange
  }: {
    field: FieldDef;
    params: Params;
    path: string;
    issues: IssueIndex;
    surface: VariableSurface;
    kindFields?: string[];
    choices?: Choice[];
    maxLines?: number;
    disabled?: boolean;
    onchange?: (name: string) => void;
  } = $props();

  const { t } = getI18n();

  const code = $derived(issueAt(issues, path));
  const errorText = $derived(code ? t(`automations.issue.${code}` as MessageKey) : '');
  const label = $derived(t(`automations.field.${field.name}` as MessageKey));
  const errId = $props.id();
  const FULL_WIDTH = new Set(['template', 'list', 'map']);
  const wide = $derived(FULL_WIDTH.has(field.kind) || field.name === 'name');

  const enumChoices = $derived(
    choices ??
      (field.values ?? []).map((value) => ({
        value,
        label: isNumericEnum(field)
          ? t('automations.unitSeconds', { n: value })
          : t(`automations.enum.${field.name}.${value || 'none'}` as MessageKey)
      }))
  );

  const lines = $derived(
    Array.isArray(params[field.name]) ? (params[field.name] as string[]).join('\n') : ''
  );

  function set(value: unknown) {
    params[field.name] = value;
    onchange?.(field.name);
  }

  function setNumber(n: number) {
    set(Number.isNaN(n) ? null : n);
  }

  function setEnum(value: string) {
    set(isNumericEnum(field) ? Number(value) : value);
  }

  function setLines(text: string) {
    set(text.split('\n').map((l) => l.trim()).filter(Boolean));
  }

  function toggleValue(value: string, on: boolean) {
    const current = (params[field.name] as string[]) ?? [];
    set(on ? [...current.filter((v) => v !== value), value] : current.filter((v) => v !== value));
  }

  function filterValue(name: string): string {
    return String((params[field.name] as Record<string, string> | undefined)?.[name] ?? '');
  }

  function setFilter(name: string, value: string) {
    const next = { ...((params[field.name] as Record<string, string>) ?? {}) };
    if (value === '') delete next[name];
    else next[name] = value;
    set(next);
  }
</script>

<div class="pf" class:wide data-kind={field.kind}>
  {#if field.kind === 'bool'}
    <div class="pf-check">
      <Checkbox checked={params[field.name] === true} {disabled} onchange={(e: Event) => set((e.currentTarget as HTMLInputElement).checked)}>{label}</Checkbox>
    </div>
  {:else if field.kind === 'template'}
    <Field label={label}>
      <ResponseEditor
        surface={surface}
        name=""
        {maxLines}
        maxlength={field.max}
        bind:value={() => String(params[field.name] ?? ''), (v) => set(v)}
        invalid={!!code}
        describedby={code ? errId : undefined}
        placeholder={t('automations.templatePh')}
      />
    </Field>
  {:else if field.kind === 'map'}
    <Field label={label}>
      <div class="pf-filters">
        {#each kindFields as name (name)}
          <label class="pf-filter">
            <Text as="span" size="xs" mono tone="muted" truncate>{name}</Text>
            <Input fill value={filterValue(name)} {disabled} invalid={!!issueAt(issues, `${path}.${name}`)} oninput={(e: Event) => setFilter(name, (e.currentTarget as HTMLInputElement).value)} />
            <span class="pf-filter-err"><Text as="span" size="xs" tone="danger">{issueAt(issues, `${path}.${name}`) ? t(`automations.issue.${issueAt(issues, `${path}.${name}`)}` as MessageKey) : ''}</Text></span>
          </label>
        {/each}
      </div>
    </Field>
  {:else if field.kind === 'list' && field.values}
    <Field label={label}>
      <div class="pf-checks">
        {#each field.values as value (value)}
          <Checkbox checked={((params[field.name] as string[]) ?? []).includes(value)} {disabled} onchange={(e: Event) => toggleValue(value, (e.currentTarget as HTMLInputElement).checked)}>
            {t(`automations.enum.${field.name}.${value}` as MessageKey)}
          </Checkbox>
        {/each}
      </div>
    </Field>
  {:else if field.kind === 'list'}
    <Field label={label} hint={t('automations.listHint')}>
      <Textarea fill rows={3} value={lines} {disabled} invalid={!!code} oninput={(e: Event) => setLines((e.currentTarget as HTMLTextAreaElement).value)} />
    </Field>
  {:else if field.kind === 'enum' || choices}
    <Field label={label}>
      <Select fill label={label} {disabled} invalid={!!code} options={enumChoices} value={String(params[field.name] ?? '')} onchange={(e) => setEnum(e.currentTarget.value)} />
    </Field>
  {:else if field.kind === 'int'}
    <Field label={label}>
      <Input fill type="number" min={field.min} max={field.max} mono {disabled} invalid={!!code} aria-describedby={code ? errId : undefined} value={params[field.name] as number} oninput={(e: Event) => setNumber((e.currentTarget as HTMLInputElement).valueAsNumber)} />
    </Field>
  {:else}
    <Field label={label}>
      <Input fill {disabled} invalid={!!code} aria-describedby={code ? errId : undefined} value={String(params[field.name] ?? '')} oninput={(e: Event) => set((e.currentTarget as HTMLInputElement).value)} />
    </Field>
  {/if}
  {#if field.kind !== 'map'}
    <span class="pf-err" id={errId} role={code ? 'alert' : undefined}><Text as="span" size="xs" tone="danger">{errorText}</Text></span>
  {/if}
</div>

<style>
  .pf { display: flex; flex-direction: column; gap: 2px; min-width: 0; --field-mb: 0; }
  .pf.wide { grid-column: 1 / -1; }
  .pf-err, .pf-filter-err { display: block; min-height: 18px; line-height: 18px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .pf-check { display: flex; align-items: center; min-height: 40px; --bb-check-align: center; }
  .pf-checks { display: flex; align-items: center; gap: 16px; min-height: 40px; }
  .pf-filters { display: flex; flex-direction: column; gap: 4px; }
  .pf-filter { display: grid; grid-template-columns: 112px minmax(0, 1fr); align-items: center; column-gap: 12px; }
  .pf-filter-err { grid-column: 2; }
</style>
