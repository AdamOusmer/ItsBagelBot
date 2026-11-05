<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Icon from './FlowIcon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import VarCard from '@bagel/ui/svelte/VarCard.svelte';
  import { getI18n } from '@bagel/kit';
  import { chipHint, pinnedFor, sheetFor, type VariableChip } from '@bagel/kit/variables';
  import { page } from '$app/state';
  import { portLabel } from '#lib/flow/describe';
  import { rehearseFlow } from '#lib/flow/rehearse';
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import { fieldLabel, fieldOptions, fieldPlaceholder, fieldUnit, stepLabel, stepSummary, variableLabel } from '#lib/flow/copy';
  import { fieldVisible, outputsFor, specFor, variablesFor, type Flow, type FlowKind } from '#lib/flow/model';
  import { duplicateNode, removeNode } from '#lib/flow/ops';
  import ChoiceField from './ChoiceField.svelte';
  import FlowRehearsal from './FlowRehearsal.svelte';
  import GoveeLightField from './GoveeLightField.svelte';
  import UserField from './UserField.svelte';

  let {
    flow = $bindable(),
    selectedId = $bindable(null),
    focusToken = 0,
    onclose
  }: { flow: Flow; selectedId?: string | null; focusToken?: number; onclose?: () => void } = $props();

  const { t } = getI18n();
  let panel = $state<HTMLElement>();

  $effect(() => {
    if (focusToken > 0 && panel) {
      const root = panel;
      void tick().then(() => {
        const fields = Array.from(root.querySelectorAll<HTMLElement>('.scroll :is(input, textarea, select, button):not([disabled], [tabindex="-1"])'));
        (fields.find((el) => el.getClientRects().length > 0) ?? root).focus();
      });
    }
  });
  const uid = $props.id();
  const KIND_TINT: Record<FlowKind, string> = {
    trigger: 'var(--bb-green-glow)',
    condition: 'var(--bb-tan)',
    reply: 'var(--bb-muted-light)'
  };

  const node = $derived(flow.nodes.find((n) => n.id === selectedId));
  const spec = $derived(node ? specFor(node.type) : undefined);
  const tokens = $derived(node ? variablesFor(flow, node.id) : []);
  const moduleFlags = $derived((page.data.moduleFlags as Record<string, boolean> | undefined) ?? {});
  const rehearsal = $derived(node ? rehearseFlow(flow, t, { until: node.id, modules: moduleFlags }) : []);

  const compareExample = $derived.by(() => {
    if (node?.type !== 'cond.compare') return null;
    const step = rehearsal.at(-1);
    if (step?.kind !== 'check') return null;
    const sentence = step.segments.map((seg) => seg.text).join('');
    const taken = step.choices.find((c) => c.id === step.taken)?.label;
    return taken ? t('flows.board.compareExample', { sentence, branch: taken }) : null;
  });

  const caret = new Map<string, number>();
  const textOf = (key: string) => String(node?.config[key] ?? '');
  const templateFields = $derived(spec?.fields.filter((f) => f.type === 'template') ?? []);

  let focusedKey = $state<string>();
  const insertKey = $derived(templateFields.find((f) => f.key === focusedKey)?.key ?? templateFields[0]?.key);

  let query = $state('');
  const needle = $derived(query.trim().toLowerCase());

  const flowVars = $derived(
    tokens
      .map((v) => ({ token: v.token, hint: t('flows.board.variableHint', { label: variableLabel(t, v), sample: v.sample }) }))
      .filter((v) => matches(v.token, v.hint))
  );

  const catalogHint = (c: VariableChip) => chipHint(t, c) ?? (c.sample ? t('flows.board.variableSample', { sample: c.sample }) : undefined);

  const channelVars = $derived.by(() => {
    const own = new Set(tokens.map((v) => v.token));
    const chips = needle ? sheetFor('custom') : pinnedFor('custom');
    return chips
      .filter((c) => !own.has(c.token))
      .map((c) => ({ token: c.token, hint: catalogHint(c) }))
      .filter((v) => matches(v.token, v.hint));
  });

  function matches(token: string, hint: string | undefined): boolean {
    return !needle || token.toLowerCase().includes(needle) || Boolean(hint?.toLowerCase().includes(needle));
  }

  function trackCaret(key: string, field: HTMLInputElement | HTMLTextAreaElement) {
    focusedKey = key;
    caret.set(key, field.selectionStart ?? field.value.length);
  }

  async function insertToken(key: string, token: string) {
    if (!node) return;
    const current = textOf(key);
    const at = Math.min(caret.get(key) ?? current.length, current.length);
    node.config[key] = current.slice(0, at) + token + current.slice(at);
    const next = at + token.length;
    caret.set(key, next);
    await tick();
    const area = document.getElementById(`${uid}-${key}`) as HTMLInputElement | HTMLTextAreaElement | null;
    area?.focus();
    area?.setSelectionRange(next, next);
  }

  function reach(from: string, port: string): number {
    const seen = new Set<string>();
    const walk = (ids: string[]) => {
      for (const id of ids) {
        if (seen.has(id)) continue;
        seen.add(id);
        walk(flow.edges.filter((e) => e.from === id).map((e) => e.to));
      }
    };
    walk(flow.edges.filter((e) => e.from === from && e.port === port).map((e) => e.to));
    return seen.size;
  }

  const branches = $derived(
    node && spec?.outputs
      ? outputsFor(spec).map((o) => ({ ...o, label: portLabel(t, node, o.id), count: reach(node.id, o.id) }))
      : []
  );

  const countText = (count: number) =>
    count === 0 ? t('flows.board.branchNone') : count === 1 ? t('flows.board.branchOne') : t('flows.board.branchMany', { count });
  const hintTitle = (token: string, hint: string | undefined) => (hint ? t('flows.board.variableTitle', { token, hint }) : token);

  function duplicate() {
    if (!node) return;
    const copy = duplicateNode(flow, node.id);
    if (copy) selectedId = copy.id;
  }

  function remove() {
    if (!node) return;
    const id = node.id;
    selectedId = null;
    removeNode(flow, id);
  }
</script>

{#if node && spec}
  <aside class="panel" aria-label={t('flows.board.settings', { label: stepLabel(t, spec) })} tabindex="-1" bind:this={panel}>
    <header class="head">
      <IconTile style="--tile: 40px; color: {KIND_TINT[spec.kind]}"><Icon name={spec.icon} size={20} /></IconTile>
      <div class="titles">
        <h2 class="name">{stepLabel(t, spec)}</h2>
        <p class="sentence">{t('flows.board.settingsSentence', { summary: stepSummary(t, spec) })}</p>
      </div>
      <IconButton label={t('flows.board.closeSettings')} size="sm" onclick={() => (onclose ? onclose() : (selectedId = null))}><Icon name="x" size={14} /></IconButton>
    </header>

    <div class="scroll">
      {#if spec.fields.length}
        <div class="fields">
          {#each spec.fields.filter((f) => fieldVisible(spec, node, f)) as field (field.key)}
            {@const label = fieldLabel(t, spec, field)}
            {@const placeholder = fieldPlaceholder(t, spec, field)}
            {#if field.type === 'color'}
              <Field {label} for="{uid}-{field.key}">
                <ColorPicker
                  id="{uid}-{field.key}"
                  value={textOf(field.key) || '#ffffff'}
                  onValueChange={(hex) => (node.config[field.key] = hex)}
                />
              </Field>
            {:else if field.type === 'toggle'}
              <div class="toggle">
                <span class="toggle-label">{label}</span>
                <Switch
                  {label}
                  checked={Boolean(node.config[field.key])}
                  onCheckedChange={(v) => (node.config[field.key] = v)}
                />
              </div>
            {:else if field.type === 'select'}
              <Field {label} for="{uid}-{field.key}">
                <Select
                  id="{uid}-{field.key}"
                  options={fieldOptions(t, spec, field)}
                  value={String(node.config[field.key] ?? field.options[0] ?? '')}
                  onchange={(e) => (node.config[field.key] = e.currentTarget.value)}
                />
              </Field>
            {:else if field.type === 'number'}
              <Field {label} for="{uid}-{field.key}">
                <Input
                  id="{uid}-{field.key}"
                  type="number"
                  min={field.min}
                  max={field.max}
                  value={node.config[field.key] === undefined ? '' : Number(node.config[field.key])}
                  oninput={(e) => {
                    const raw = e.currentTarget.value;
                    if (raw === '') delete node.config[field.key];
                    else node.config[field.key] = Number(raw);
                  }}
                >
                  {#snippet trailing()}{#if field.unit}<span class="unit">{fieldUnit(t, spec, field)}</span>{/if}{/snippet}
                </Input>
              </Field>
            {:else if field.type === 'template' && field.singleLine}
              <Field {label} for="{uid}-{field.key}">
                <Input
                  id="{uid}-{field.key}"
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => {
                    node.config[field.key] = e.currentTarget.value;
                    trackCaret(field.key, e.currentTarget);
                  }}
                  onfocus={(e) => trackCaret(field.key, e.currentTarget)}
                  onblur={(e) => trackCaret(field.key, e.currentTarget)}
                  onclick={(e) => trackCaret(field.key, e.currentTarget)}
                  onkeyup={(e) => trackCaret(field.key, e.currentTarget)}
                />
              </Field>
            {:else if field.type === 'template'}
              <Field {label} for="{uid}-{field.key}">
                <Textarea
                  id="{uid}-{field.key}"
                  rows={4}
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => {
                    node.config[field.key] = e.currentTarget.value;
                    trackCaret(field.key, e.currentTarget);
                  }}
                  onfocus={(e) => trackCaret(field.key, e.currentTarget)}
                  onblur={(e) => trackCaret(field.key, e.currentTarget)}
                  onclick={(e) => trackCaret(field.key, e.currentTarget)}
                  onkeyup={(e) => trackCaret(field.key, e.currentTarget)}
                />
              </Field>
            {:else if field.type === 'choice'}
              <ChoiceField {node} {field} {label} id="{uid}-{field.key}" />
            {:else if field.type === 'goveeLight'}
              <GoveeLightField {node} id="{uid}-{field.key}" />
            {:else if field.type === 'login'}
              {#key node.id}
                <UserField {node} id="{uid}-{field.key}" {label} {placeholder} />
              {/key}
            {:else}
              <Field {label} for="{uid}-{field.key}">
                <Input
                  id="{uid}-{field.key}"
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => (node.config[field.key] = e.currentTarget.value)}
                />
              </Field>
            {/if}
          {/each}
        </div>
      {/if}

      {#if spec.kind !== 'condition'}
        <FlowRehearsal steps={rehearsal} focusId={node.id} />
      {/if}

      {#if branches.length}
        <section class="group">
          <h3 class="group-title">{t('flows.board.branchesTitle')}</h3>
          {#if compareExample}
            <p class="note">{compareExample}</p>
          {/if}
          <ul class="branches">
            {#each branches as b (b.id)}
              <li data-port={b.id}>
                <span class="branch-name">{b.label}</span>
                <span class="branch-count">{countText(b.count)}</span>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if insertKey}
        <section class="group">
          <h3 class="group-title">{t('flows.board.variables')}</h3>
          <SearchInput fill bind:value={query} placeholder={t('flows.board.searchVariables')} />
          {#if flowVars.length}
            <h4 class="subgroup-title">{t('flows.board.variablesFlow')}</h4>
            <ul class="vars">
              {#each flowVars as v (v.token)}
                <li><VarCard stacked token={v.token} hint={v.hint} title={hintTitle(v.token, v.hint)} onclick={() => insertToken(insertKey, v.token)} /></li>
              {/each}
            </ul>
          {:else if !tokens.length && !needle}
            <p class="note">{t('flows.board.variablesConnect')}</p>
          {/if}
          {#if channelVars.length}
            <h4 class="subgroup-title">{needle ? t('flows.board.variablesAll') : t('flows.board.variablesPopular')}</h4>
            <ul class="vars">
              {#each channelVars as v (v.token)}
                <li><VarCard stacked token={v.token} hint={v.hint} title={hintTitle(v.token, v.hint)} onclick={() => insertToken(insertKey, v.token)} /></li>
              {/each}
            </ul>
          {/if}
          {#if needle && !flowVars.length && !channelVars.length}
            <p class="note">{t('flows.board.variablesNone', { query: query.trim() })}</p>
          {/if}
        </section>
      {/if}
    </div>

    <footer class="foot">
      <Button variant="ghost" onclick={duplicate}>{t('flows.board.duplicate')}</Button>
      <Button tone="danger" onclick={remove}>{t('flows.board.removeStep')}</Button>
    </footer>
  </aside>
{/if}

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    border-radius: 12px;
    background: var(--bb-bg-1);
    box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65);
  }
  .head {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    flex: none;
    padding: 16px 12px 8px 16px;
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 17px;
    font-weight: 600;
    line-height: 1.3;
    color: var(--bb-white);
  }
  .sentence {
    margin: 2px 0 0;
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 28px;
    padding: 20px 16px;
  }
  .scroll > :global(*) {
    flex: none;
  }
  .fields {
    display: flex;
    flex-direction: column;
    gap: 20px;
    --field-mb: 0;
    --field-gap: 8px;
  }
  .fields :global(label) {
    font-size: 14px;
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .fields :global(input),
  .fields :global(textarea),
  .fields :global(select) {
    font-size: 15px;
  }
  .unit {
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .toggle-label {
    font-size: 15px;
    color: var(--bb-white);
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .group-title {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .branches {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .branches li {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }
  .branch-name {
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .branches li:first-child .branch-name {
    color: var(--bb-green-glow);
  }
  .branch-count {
    margin-inline-start: auto;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .subgroup-title {
    margin: 4px 0 0;
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .vars {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .note {
    margin: 0;
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex: none;
    padding: 12px 16px;
    background: var(--bb-bg-1);
    box-shadow: 0 -8px 16px rgba(var(--bb-shadow-rgb), 0.3);
  }
</style>
