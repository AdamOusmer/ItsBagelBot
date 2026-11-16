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
  import { getI18n, type MessageKey } from '@bagel/kit';
  import { chipHint, pinnedFor, sheetFor, variableById, type VariableChip } from '@bagel/kit/variables';
  import { portLabel } from '#lib/flow/describe';
  import { fieldRequired, type FieldIssue } from '#lib/flow/issues';
  import type { RehearsalView } from '#lib/flow/rehearse';
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import DurationField from '#lib/components/shared/DurationField.svelte';
  import { durationOutside, durationRange, fieldIssueMessage, fieldLabel, fieldOptions, fieldPlaceholder, fieldUnit, stepLabel, stepSummary, variableLabel } from '#lib/flow/copy';
  import { fieldVisible, NODE_SPECS, outputsFor, specFor, variablesFor, type FieldSpec, type Flow, type FlowKind, type FlowNode, type NodeSpec } from '#lib/flow/model';
  import { duplicateNode } from '#lib/flow/ops';
  import { awaitingSetup, configText } from '#lib/flow/setup';
  import { SETUP_URL } from '@bagel/kit/automations';
  import ChoiceField from './ChoiceField.svelte';
  import FlowRehearsal from './FlowRehearsal.svelte';
  import GoveeLightField from './GoveeLightField.svelte';
  import RewardField from './RewardField.svelte';
  import UserField from './UserField.svelte';
  import TokenSuggestions from './TokenSuggestions.svelte';

  let {
    flow = $bindable(),
    selectedId = $bindable(null),
    rehearsal,
    focusToken = 0,
    focusField,
    issues = [],
    onclose,
    onremove
  }: { flow: Flow; selectedId?: string | null; rehearsal: RehearsalView; focusToken?: number; focusField?: string; issues?: FieldIssue[]; onclose?: () => void; onremove: (id: string) => void } = $props();

  const { t } = getI18n();
  type FieldKey = FieldSpec['key'];
  type Token = VariableChip['token'];
  type Hint = string | undefined;
  let panel = $state<HTMLElement>();

  $effect(() => {
    if (focusToken > 0 && panel) {
      const root = panel;
      const key = focusField;
      void tick().then(() => {
        const field = Array.from(root.querySelectorAll<HTMLElement>('[data-field]')).find((el) => el.dataset.field === key);
        const control = field?.querySelector<HTMLElement>('input, textarea, select, button');
        const fields = Array.from(root.querySelectorAll<HTMLElement>('.scroll :is(input, textarea, select, button):not([disabled], [tabindex="-1"])'));
        (control ?? field ?? fields.find((el) => el.getClientRects().length > 0) ?? root).focus();
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
  const targetMissing = $derived(['reply.shoutout', 'reply.timeout'].includes(node?.type ?? '') && !String(node?.config.target ?? '').trim());

  const compareExample = $derived.by(() => {
    if (node?.type !== 'cond.compare') return null;
    const step = rehearsal.steps.find((step) => step.id === node.id);
    if (step?.kind !== 'check') return null;
    return t('flows.board.compareExample', { sentence: step.text, branch: portLabel(t, node, step.port ?? 'no') });
  });

  const caret = new Map<string, number>();
  const textOf = (key: FieldKey) => (node ? configText(node, key) : '');
  const awaiting = $derived(node ? awaitingSetup(node) : false);

  let heldForSetup: string | null = null;
  $effect(() => {
    if (!node) return;
    if (awaiting) heldForSetup = node.id;
    else if (heldForSetup === node.id) {
      heldForSetup = null;
      delete node.config.disabled;
    }
  });

  $effect(() => {
    if (!node || spec?.type !== 'cond.cooldown') return;
    if (node.config.perUser && node.config.name) delete node.config.name;
  });

  const templateFields = $derived(node && spec ? spec.fields.filter((f) => f.type === 'template' && fieldVisible(spec, node, f)) : []);

  let focusedKey = $state<string>();
  type TextControl = HTMLInputElement | HTMLTextAreaElement;
  let completion = $state<{ key: FieldKey; start: number; end: number; query: string; anchor: TextControl } | null>(null);
  let activeSuggestion = $state(0);
  const contextualTokens = new Set(NODE_SPECS.flatMap((entry) => [...entry.provides ?? [], ...entry.outputs?.flatMap((output) => output.provides ?? []) ?? []]).map((v) => v.token));
  const channelChips = sheetFor('custom').filter((v) => !contextualTokens.has(v.token) && Boolean(chipHint(t, v)));
  const suggestions = $derived.by(() => {
    const search = completion?.query.toLowerCase() ?? '';
    const available = [
      ...tokens.map((v) => ({ token: v.token, label: variableLabel(t, v) })),
      ...channelChips.map((v) => ({ token: v.token, label: catalogLabel(v) }))
    ];
    const unique = new Map(available.map((v) => [v.token, v]));
    return [...unique.values()].filter((v) => `${v.token} ${v.label}`.toLowerCase().includes(search));
  });
  $effect(() => {
    void node?.id;
    caret.clear();
    focusedKey = undefined;
    query = '';
    completion = null;
  });
  const insertKey = $derived(templateFields.find((f) => f.key === focusedKey)?.key ?? templateFields[0]?.key);
  const tokens = $derived(node ? [
    ...variablesFor(flow, node.id).filter((v) => !(node.type === 'reply.countdown_start' && insertKey === 'leftReply' && v.token === '{countdown:left}')),
    ...(node.type === 'reply.countdown_start' && insertKey === 'leftReply' ? [{ token: '{countdown:left}', name: 'countdownLeft', sample: '2d 3h' }] : [])
  ] : []);

  let query = $state('');
  const needle = $derived(query.trim().toLowerCase());

  const flowVars = $derived(
    tokens
      .map((v) => ({ token: v.token, label: variableLabel(t, v), hint: t('flows.board.variableSample', { sample: v.sample }) }))
      .filter((v) => matches(v.token, `${v.label} ${v.hint}`))
  );

  const channelVars = $derived.by(() => {
    const chips = needle ? sheetFor('custom') : pinnedFor('custom');
    return chips
      .filter((c) => !contextualTokens.has(c.token) && Boolean(chipHint(t, c)))
      .map((c) => ({ token: c.token, label: catalogLabel(c), hint: catalogSample(c) }))
      .filter((v) => matches(v.token, `${v.label} ${v.hint}`));
  });

  function catalogLabel(chip: VariableChip): string {
    return chip.id ? t(`vars.${chip.id}.name` as MessageKey) : chipHint(t, chip) ?? '';
  }

  function catalogSample(chip: VariableChip): string | undefined {
    const forms = variableById(chip.id ?? '')?.forms ?? [];
    const sample = chip.sample ?? forms.find((form) => form.example === chip.token)?.output ?? forms[0]?.output;
    return sample ? t('flows.board.variableSample', { sample }) : undefined;
  }

  function matches(token: Token, hint: Hint): boolean {
    return !needle || token.toLowerCase().includes(needle) || Boolean(hint?.toLowerCase().includes(needle));
  }

  function trackCaret(key: FieldKey, field: HTMLInputElement | HTMLTextAreaElement) {
    focusedKey = key;
    caret.set(key, field.selectionStart ?? field.value.length);
    if (completion?.key === key && completion.end !== field.selectionStart) completion = null;
  }

  function updateTemplate(key: FieldKey, control: TextControl) {
    if (!node) return;
    node.config[key] = control.value;
    trackCaret(key, control);
    const end = control.selectionStart ?? control.value.length;
    const match = /\{([^{}\s]*)$/.exec(control.value.slice(0, end));
    completion = match ? { key, start: end - match[0].length, end, query: match[1], anchor: control } : null;
    activeSuggestion = 0;
  }

  function suggestionKey(event: KeyboardEvent) {
    if (!completion || event.isComposing) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      completion = null;
      return;
    }
    const direction = { ArrowDown: 1, ArrowUp: -1 }[event.key];
    if (direction) {
      event.preventDefault();
      activeSuggestion = (activeSuggestion + direction + suggestions.length) % Math.max(1, suggestions.length);
      return;
    }
    if (['Enter', 'Tab'].includes(event.key) && suggestions[activeSuggestion]) {
      event.preventDefault();
      void insertToken(completion.key, suggestions[activeSuggestion].token);
    }
  }

  async function insertToken(key: FieldKey, token: Token) {
    if (!node) return;
    const current = textOf(key);
    const partial = completion?.key === key ? completion : null;
    const at = partial?.start ?? Math.min(caret.get(key) ?? current.length, current.length);
    node.config[key] = current.slice(0, at) + token + current.slice(partial?.end ?? at);
    completion = null;
    const next = at + token.length;
    caret.set(key, next);
    await tick();
    const area = document.getElementById(`${uid}-${key}`) as HTMLInputElement | HTMLTextAreaElement | null;
    area?.focus();
    area?.setSelectionRange(next, next);
  }

  function reach(from: FlowNode['id'], port: NonNullable<NodeSpec['outputs']>[number]['id']): number {
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
  const hintTitle = (token: Token, hint: Hint) => (hint ? t('flows.board.variableTitle', { token, hint }) : token);

  const RESERVE_SPACE = new Set(['cond.cooldown:name']);
  function renderVisible(s: NodeSpec, n: FlowNode, field: FieldSpec): boolean {
    return RESERVE_SPACE.has(`${s.type}:${field.key}`) || fieldVisible(s, n, field);
  }

  function duplicate() {
    if (!node) return;
    const copy = duplicateNode(flow, node.id);
    if (copy) selectedId = copy.id;
  }

  function remove() {
    if (!node) return;
    onremove(node.id);
  }
</script>

{#if completion && suggestions.length}
  <TokenSuggestions id={uid} anchor={completion.anchor} items={suggestions} active={activeSuggestion} label={t('flows.board.variables')} onselect={(token) => completion && insertToken(completion.key, token)} />
{/if}

{#if node && spec}
  <aside class="panel" aria-label={t('flows.board.settings', { label: stepLabel(t, spec) })} tabindex="-1" bind:this={panel}>
    <header class="head">
      <IconTile style="--tile: 40px; color: {KIND_TINT[spec.kind]}"><Icon name={spec.icon} size={20} /></IconTile>
      <div class="titles">
        <h2 class="name"><bb-fit><span data-fit>{stepLabel(t, spec)}</span></bb-fit></h2>
        <p class="sentence"><bb-fit><span data-fit>{t('flows.board.settingsSentence', { summary: stepSummary(t, spec) })}</span></bb-fit></p>
      </div>
      <IconButton label={t('flows.board.closeSettings')} size="sm" onclick={() => (onclose ? onclose() : (selectedId = null))}><Icon name="x" size={14} /></IconButton>
    </header>

    <div class="scroll">
      {#if awaiting}
        <p class="note">{t('flows.setup.stepNote')}</p>
      {/if}
      {#if spec.fields.length}
        <div class="fields">
          {#each spec.fields.filter((f) => renderVisible(spec, node, f)) as field (field.key)}
            {@const label = fieldLabel(t, spec, field)}
            {@const issue = issues.find((issue) => issue.nodeId === node.id && issue.fieldKey === field.key)}
            {@const error = issue ? fieldIssueMessage(t, issue, node) : undefined}
            {@const tag = fieldRequired(node, field.key) ? t('flows.clarity.required') : undefined}
            {@const placeholder = node.config[field.key] === SETUP_URL ? t('flows.setup.addUrl') : fieldPlaceholder(t, spec, field)}
            <div class="field" data-field={field.key} role="group" aria-label={label} tabindex="-1">
            {#if spec.type === 'cond.cooldown' && field.key === 'name'}
              {@const perViewer = Boolean(node.config.perUser)}
              <Field {label} {tag} {error} for="{uid}-{field.key}" hint={t('flows.step.condition.cooldown.field.name.hint')}>
                <Input id="{uid}-{field.key}" {placeholder} value={perViewer ? '' : textOf(field.key)} disabled={perViewer} oninput={(e) => (node.config[field.key] = e.currentTarget.value)} />
              </Field>
            {:else if field.type === 'color'}
              <Field {label} {tag} {error} for="{uid}-{field.key}">
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
                  checked={node.config[field.key] === undefined ? field.defaultOn === true : Boolean(node.config[field.key])}
                  onCheckedChange={(v) => (node.config[field.key] = v)}
                />
              </div>
            {:else if field.type === 'select'}
              <Field {label} {tag} {error} for="{uid}-{field.key}">
                <Select
                  id="{uid}-{field.key}"
                  options={fieldOptions(t, spec, field)}
                  value={String(node.config[field.key] ?? field.options[0] ?? '')}
                  onchange={(e) => (node.config[field.key] = e.currentTarget.value)}
                />
              </Field>
            {:else if field.type === 'number'}
              <Field {label} {tag} {error} for="{uid}-{field.key}" hint={spec.type === 'reply.cooldown_add' && field.key === 'per' ? t('flows.step.reply.cooldownAdd.field.per.hint') : spec.type === 'reply.countdown_add' && field.key === 'per' ? t('flows.step.reply.countdownAdd.field.per.hint') : undefined}>
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
            {:else if field.type === 'duration'}
              {@const outside = durationOutside(field, node.config[field.key])}
              {@const range = durationRange(t, spec, field)}
              <Field {label} {tag} hint={outside ? undefined : range} error={error ?? (outside ? range : undefined)} errorId="{uid}-{field.key}-error">
                {#key node.id}
                  <DurationField
                    id="{uid}-{field.key}"
                    min={field.min}
                    max={field.max}
                    {label}
                    invalid={outside || Boolean(error)}
                    describedby={outside || error ? `${uid}-${field.key}-error` : undefined}
                    bind:value={
                      () => (node.config[field.key] === undefined ? Number.NaN : Number(node.config[field.key])),
                      (seconds) => {
                        if (Number.isFinite(seconds)) node.config[field.key] = seconds;
                        else delete node.config[field.key];
                      }
                    }
                  />
                {/key}
              </Field>
            {:else if field.type === 'template' && field.singleLine}
              {@const needsTarget = targetMissing && field.key === 'target'}
              {@const shown = error ?? (needsTarget ? t('flows.test.noTarget') : undefined)}
              <Field {label} {tag} error={shown} for="{uid}-{field.key}" errorId="{uid}-{field.key}-error">
                <Input
                  id="{uid}-{field.key}"
                  required={field.key === 'target'}
                  aria-invalid={shown ? true : undefined}
                  aria-describedby={shown ? `${uid}-${field.key}-error` : undefined}
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => updateTemplate(field.key, e.currentTarget)}
                  onfocus={(e) => trackCaret(field.key, e.currentTarget)}
                  onblur={(e) => { trackCaret(field.key, e.currentTarget); completion = null; }}
                  onclick={(e) => trackCaret(field.key, e.currentTarget)}
                  onkeyup={(e) => trackCaret(field.key, e.currentTarget)}
                  onkeydown={suggestionKey}
                  aria-autocomplete="list"
                  aria-controls={completion?.key === field.key ? `${uid}-suggestions` : undefined}
                  aria-activedescendant={completion?.key === field.key ? `${uid}-suggestion-${activeSuggestion}` : undefined}
                />
              </Field>
            {:else if field.type === 'template'}
              <Field {label} {tag} {error} for="{uid}-{field.key}">
                <Textarea
                  id="{uid}-{field.key}"
                  rows={4}
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => updateTemplate(field.key, e.currentTarget)}
                  onfocus={(e) => trackCaret(field.key, e.currentTarget)}
                  onblur={(e) => { trackCaret(field.key, e.currentTarget); completion = null; }}
                  onclick={(e) => trackCaret(field.key, e.currentTarget)}
                  onkeyup={(e) => trackCaret(field.key, e.currentTarget)}
                  onkeydown={suggestionKey}
                  aria-autocomplete="list"
                  aria-controls={completion?.key === field.key ? `${uid}-suggestions` : undefined}
                  aria-activedescendant={completion?.key === field.key ? `${uid}-suggestion-${activeSuggestion}` : undefined}
                />
              </Field>
            {:else if field.type === 'choice'}
              <ChoiceField {node} {field} {label} {tag} {error} draft={flow.nodes} id="{uid}-{field.key}" />
            {:else if field.type === 'reward'}
              <RewardField {node} {label} {tag} {error} id="{uid}-{field.key}" />
            {:else if field.type === 'goveeLight'}
              <GoveeLightField {node} {tag} fieldError={error} id="{uid}-{field.key}" />
            {:else if field.type === 'login'}
              {#key node.id}
                <UserField {node} id="{uid}-{field.key}" {label} {placeholder} {tag} {error} />
              {/key}
            {:else}
              <Field {label} {tag} {error} for="{uid}-{field.key}">
                <Input
                  id="{uid}-{field.key}"
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => (node.config[field.key] = e.currentTarget.value)}
                />
              </Field>
            {/if}
            </div>
          {/each}
        </div>
      {/if}

      {#if spec.kind !== 'condition'}
        <FlowRehearsal steps={rehearsal.steps} focusId={node.id} title={['reply.chat', 'reply.announce', 'reply.reply'].includes(node.type) ? t('chatPreview.rehearsal') : t('flows.board.whatHappens')} />
      {/if}

      {#if branches.length}
        <section class="group">
          <h3 class="group-title">{t('flows.board.branchesTitle')}</h3>
          {#if compareExample}
            <p class="note">{compareExample}</p>
          {/if}
          <ul class="branches">
            {#each branches as b (b.id)}
              <li class="branch">
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
                <li><VarCard stacked token={v.label} hint={v.hint} title={hintTitle(v.token, v.hint)} onclick={() => insertToken(insertKey, v.token)} /></li>
              {/each}
            </ul>
          {:else if !tokens.length && !needle}
            <p class="note">{t('flows.board.variablesConnect')}</p>
          {/if}
          {#if channelVars.length}
            <h4 class="subgroup-title">{needle ? t('flows.board.variablesAll') : t('flows.board.variablesPopular')}</h4>
            <ul class="vars">
              {#each channelVars as v (v.token)}
                <li><VarCard stacked token={v.label} hint={v.hint} title={hintTitle(v.token, v.hint)} onclick={() => insertToken(insertKey, v.token)} /></li>
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
    height: 76px;
    box-sizing: border-box;
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
    height: 23px;
    white-space: nowrap;
  }
  .sentence {
    margin: 2px 0 0;
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
    height: 20px;
    white-space: nowrap;
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
  .branch {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }
  .branch-name {
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .branch:first-child .branch-name {
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
