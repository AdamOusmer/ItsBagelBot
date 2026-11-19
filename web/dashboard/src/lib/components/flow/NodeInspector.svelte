<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from './FlowIcon.svelte';
  import UiIcon from '@bagel/ui/svelte/Icon.svelte';
  import '@bagel/ui/styles/elements/field-sheet.css';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Slider from '@bagel/ui/svelte/Slider.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import VarCard from '@bagel/ui/svelte/VarCard.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import { chipHint, pinnedFor, sheetFor, variableById, type VariableChip } from '@bagel/kit/variables';
  import { describe, portLabel } from '#lib/flow/describe';
  import { stepOutcome } from '#lib/flow/rehearsal-view';
  import { fieldRequired, inlineFieldIssue, type FieldIssue } from '#lib/flow/issues';
  import type { RehearsalView } from '#lib/flow/rehearse';
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import DurationField from '#lib/components/shared/DurationField.svelte';
  import { checkNote, durationOutside, durationRange, fieldHint, fieldIssueMessage, fieldLabel, fieldOptions, fieldPlaceholder, fieldUnit, hasCheckNote, requirementText, requirementTrigger, roleWarning, scopeWarning, stepLabel, stepNote, stepRequirementFor, stepSummary, targetOptions, variableLabel } from '#lib/flow/copy';
  import { defaultComparison } from '#lib/flow/comparison';
  import { fieldMounted, fieldVisible, flowTrigger, migrateCooldownConfig, NODE_SPECS, outputsFor, specFor, storedVariable, targetFieldFor, targetModesFor, variablesFor, type FieldSpec, type Flow, type FlowKind, type FlowNode, type NodeSpec } from '#lib/flow/model';
  import { duplicateNode, hasCheckBefore, insertCheckBefore, updateNodeConfig } from '#lib/flow/ops';
  import { descendants, graphIndex, portKey } from '#lib/flow/graph-index';
  import { awaitingSetup, configText } from '#lib/flow/setup';
  import { canNameCooldown, cooldownWindows, hasRedemptionStep, pointsChange, SETUP_URL, triggerAmountUnit } from '@bagel/kit/automations';
  import ChoiceField from './ChoiceField.svelte';
  import FlowRehearsal from './FlowRehearsal.svelte';
  import RunOutcome from './RunOutcome.svelte';
  import GoveeLightField from './GoveeLightField.svelte';
  import RewardField from './RewardField.svelte';
  import UserField from './UserField.svelte';
  import TokenSuggestions from './TokenSuggestions.svelte';
  import WebReadTryIt from './WebReadTryIt.svelte';

  let {
    flow = $bindable(),
    selectedId = $bindable(null),
    fallbackId,
    active = true,
    rehearsal,
    focusToken = 0,
    focusField,
    issues = [],
    permissionRank = -1,
    onclose,
    onremove,
    oncheck,
    ontest,
    onResponseSample
  }: { flow: Flow; selectedId?: string | null; fallbackId?: string; active?: boolean; rehearsal: RehearsalView; focusToken?: number; focusField?: string; issues?: FieldIssue[]; permissionRank?: number; onclose?: () => void; onremove: (id: string, keepYes?: boolean) => void; oncheck?: () => void; ontest?: () => void; onResponseSample?: (nodeId: FlowNode['id'], sample: string) => void } = $props();

  const { t } = getI18n();
  type FieldKey = FieldSpec['key'];
  type Token = VariableChip['token'];
  type Hint = string | undefined;
  let panel = $state<HTMLElement>();
  const fieldElements: Record<FieldKey, HTMLElement | undefined> = {};

  $effect(() => {
    if (!active) return;
    if (focusToken <= 0) return;
    const root = panel;
    if (!root) return;
    const key = focusField;
    void tick().then(() => {
      const target = key ? fieldElements[key] : undefined;
      const field = target?.inert ? undefined : target;
      const control = field?.querySelector<HTMLElement>('input, textarea, select, button');
      const fields = Array.from(root.querySelectorAll<HTMLElement>('.scroll :is(input, textarea, select, button):not(:disabled, [tabindex="-1"], [inert] *)'));
      (control ?? field ?? fields.find((el) => el.getClientRects().length > 0) ?? root).focus();
    });
  });
  const uid = $props.id();
  const KIND_TINT: Record<FlowKind, string> = {
    trigger: 'var(--bb-green-glow)',
    condition: 'var(--bb-tan)',
    reply: 'var(--bb-muted-light)'
  };

  const node = $derived(flow.nodes.find((n) => n.id === (selectedId ?? fallbackId)));
  const spec = $derived(node ? specFor(node.type) : undefined);
  const durationConfig = $derived(node?.type === 'cond.cooldown' ? migrateCooldownConfig(node.config) : node?.config);
  const namedCooldownDrafts = $derived(flow.nodes.filter((entry) => entry.type === 'cond.cooldown' && canNameCooldown(cooldownWindows(entry.config))));
  const trigger = $derived(flowTrigger(flow));
  const amountUnit = $derived(triggerAmountUnit(trigger));
  const lastStep = $derived(node ? rehearsal.steps.findLast((step) => step.id === node.id) : undefined);
  const summary = $derived(node ? describe(t, node, trigger) : '');
  const lastText = $derived(lastStep?.text || lastStep?.lines?.join(' ') || t('flows.test.noResult'));

  const compareExample = $derived.by(() => {
    if (node?.type !== 'cond.compare') return null;
    const step = rehearsal.steps.find((step) => step.id === node.id);
    if (step?.kind !== 'check') return null;
    return t('flows.board.compareExample', { sentence: step.text, branch: portLabel(t, node, step.port ?? 'no') });
  });

  const caret = new Map<string, number>();
  const textOf = (key: FieldKey) => (node ? configText(node, key) : '');
  const awaiting = $derived(node ? awaitingSetup(node) : false);
  const requirement = $derived(node && spec && stepRequirementFor(
    spec, { ...requirementTrigger(flow), perm: permissionRank }, { who: node.config.who }
  ));
  const roleLevel = $derived(requirement?.kind === 'role' ? requirement.level : undefined);
  const incoming = $derived(node ? graphIndex(flow).incoming.get(node.id) ?? [] : []);
  const addedCheck = $derived(roleLevel ? hasCheckBefore(flow, node?.id ?? '', roleLevel) : false);
  const removeCheck = $derived(roleWarning(t, flow, node));
  const roleNote = $derived(requirement ? requirementText(t, requirement, addedCheck ? 'start' : 'note', spec) : removeCheck ?? scopeWarning(t, flow, node));
  const variableToken = $derived(node?.type === 'reply.var' ? storedVariable(node.config)[0]?.token : undefined);
  const patternSample = $derived(node?.type === 'cond.pattern' ? rehearsal.steps.find((step) => step.id === node?.id && step.kind === 'check')?.text : undefined);
  const passingNote = $derived(patternSample ?? checkNote(t, node, flow));
  const canAddCheck = $derived(roleLevel ? incoming.length > 0 && !addedCheck : false);
  const checkAction = $derived(canAddCheck || Boolean(removeCheck));
  const automaticComparisons = new Map<FlowNode['id'], boolean>();
  const legacyTake = $derived(node?.type === 'reply.points' && node.config.change === undefined && pointsChange(node.config) === 'take');

  function setOption(key: FieldKey, value: string | boolean) {
    if (!node || !spec) return;
    if (key === 'change') prepareChange(node, String(value));
    const prior = { config: { ...node.config } };
    updateNodeConfig(flow, node, { [key]: value });
    const withDefaults = spec.fields.filter((field) => field.initial !== undefined);
    const newlyVisible = withDefaults.filter((field) => !fieldVisible(spec, prior, field) && fieldVisible(spec, node, field));
    for (const field of newlyVisible) node.config[field.key] ??= field.initial!;
    if (node.type === 'cond.compare') automaticComparisons.set(node.id, false);
  }

  function prepareChange(node: FlowNode, change: string) {
    if (legacyTake) node.config.amount = Math.abs(Number(node.config.amount));
    if (node.type === 'reply.counter') setCounterDefault(node, change);
  }

  function selectValue(node: FlowNode, field: Extract<FieldSpec, { type: 'select' }>): string {
    if (field.key === 'change' && legacyTake) return 'take';
    return String(node.config[field.key] ?? field.fallback ?? field.initial ?? field.options[0] ?? '');
  }

  function setDuration(node: FlowNode, key: FieldKey, seconds: number) {
    const config = node.type === 'cond.cooldown' ? migrateCooldownConfig(node.config) : node.config;
    if (Number.isFinite(seconds)) config[key] = seconds;
    else delete config[key];
    if (config !== node.config) node.config = config;
  }

  function setCounterDefault(node: FlowNode, change: string) {
    const key = change === 'set' ? 'value' : 'amount';
    const initial = spec?.fields.find((field) => field.key === key)?.initial;
    if (typeof initial === 'number') node.config[key] ??= initial;
  }

  function setNumber(key: FieldKey, raw: string) {
    if (!node) return;
    if (key === 'amount' && legacyTake) node.config.change = 'take';
    if (key === 'amount' && node.type === 'reply.counter') node.config.change ??= 'add';
    if (raw === '') delete node.config[key];
    else node.config[key] = Number(raw);
  }

  function numberValue(node: FlowNode, key: FieldKey): number | '' {
    if (key === 'amount' && legacyTake) return Math.abs(Number(node.config.amount));
    const value = node.config[key];
    if (value !== undefined) return Number(value);
    return key === 'amount' && node.type === 'reply.counter' && node.config.change === undefined ? 1 : '';
  }

  function setTemplate(key: FieldKey, value: string) {
    if (!node) return;
    if (!automaticComparisons.has(node.id)) automaticComparisons.set(node.id, !node.config.left && !node.config.right);
    node.config[key] = value;
    if (node.type !== 'cond.compare' || !automaticComparisons.get(node.id)) return;
    node.config.op = defaultComparison(flow, node);
  }

  let heldForSetup: string | null = null;
  $effect(() => {
    if (!node) return;
    if (awaiting) heldForSetup = node.id;
    else if (heldForSetup === node.id) {
      heldForSetup = null;
      delete node.config.disabled;
    }
  });

  const templateFields = $derived(spec?.fields.filter((field) => field.type === 'template') ?? []);
  const activeTemplates = $derived(node && spec ? templateFields.filter((field) => fieldVisible(spec, node, field, trigger)) : []);

  let focusedKey = $state<string>();
  type TextControl = HTMLInputElement | HTMLTextAreaElement;
  let completion = $state<{ key: FieldKey; start: number; end: number; query: string; anchor: TextControl } | null>(null);
  let activeSuggestion = $state(0);
  const contextualTokens = new Set(NODE_SPECS.flatMap((entry) => [...entry.provides ?? [], ...outputsFor(entry).flatMap((output) => output.provides ?? [])]).map((v) => v.token));
  const channelChips = sheetFor('custom').filter((v) => !contextualTokens.has(v.token) && Boolean(chipHint(t, v)));
  const suggestions = $derived.by(() => {
    const search = completion?.query.toLowerCase() ?? '';
    const available = [
      ...tokens.map((v) => ({ token: v.token, label: variableLabel(t, v) })),
      ...(restrictedTokens ? [] : channelChips.map((v) => ({ token: v.token, label: catalogLabel(v) })))
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
  const insertField = $derived(activeTemplates.find((field) => field.key === focusedKey) ?? activeTemplates[0]);
  const insertKey = $derived(insertField?.key);
  const restrictedTokens = $derived(insertField?.tokens);
  const tokens = $derived(restrictedTokens ?? (node ? variablesFor(flow, node.id) : []));

  let query = $state('');
  const needle = $derived(query.trim().toLowerCase());

  const flowVars = $derived(
    tokens
      .map((v) => ({ token: v.token, label: variableLabel(t, v), hint: t('flows.board.variableSample', { sample: v.sample }) }))
      .filter((v) => matches(v.token, `${v.label} ${v.hint}`))
  );

  const channelVars = $derived.by(() => {
    if (restrictedTokens) return [];
    const chips = needle ? sheetFor('custom') : pinnedFor('custom');
    return chips
      .filter((c) => !contextualTokens.has(c.token) && Boolean(chipHint(t, c)))
      .map((c) => ({ token: c.token, label: catalogLabel(c), hint: catalogSample(c) }))
      .filter((v) => matches(v.token, `${v.label} ${v.hint}`));
  });
  const noVariableMatches = $derived(Boolean(needle) && flowVars.length + channelVars.length === 0);

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
    setTemplate(key, control.value);
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
    const end = partial ? partial.end + (/^[^{}\s]*\}/.exec(current.slice(partial.end))?.[0].length ?? 0) : at;
    setTemplate(key, current.slice(0, at) + token + current.slice(end));
    completion = null;
    const next = at + token.length;
    caret.set(key, next);
    await tick();
    const area = document.getElementById(`${uid}-${key}`) as HTMLInputElement | HTMLTextAreaElement | null;
    area?.focus();
    area?.setSelectionRange(next, next);
  }

  function reach(from: FlowNode['id'], port: NonNullable<NodeSpec['outputs']>[number]['id']): number {
    const starts = (graphIndex(flow).ports.get(portKey({ from, port })) ?? []).map((edge) => edge.to);
    return descendants(flow, starts).size;
  }

  const branches = $derived.by(() => {
    const ports = outputsFor(spec, node);
    if (!node || ports.length < 2) return [];
    return ports.map((out) => ({ ...out, label: portLabel(t, node, out.id), count: reach(node.id, out.id) }));
  });
  const staticNote = $derived(spec ? stepNote(t, spec) : undefined);

  const countText = (count: number) =>
    count === 0 ? t('flows.board.branchNone') : count === 1 ? t('flows.board.branchOne') : t('flows.board.branchMany', { count });
  const hintTitle = (token: Token, hint: Hint) => (hint ? t('flows.board.variableTitle', { token, hint }) : token);

  function duplicate() {
    if (!node) return;
    const copy = duplicateNode(flow, node.id);
    if (copy) selectedId = copy.id;
  }

  function remove() {
    if (!node) return;
    onremove(node.id);
  }

  const OWN_ERROR_TYPES = new Set<FieldSpec['type']>(['choice', 'target', 'reward', 'goveeLight', 'login', 'duration', 'toggle']);
  function sharesNote(field: FieldSpec, hint: Hint): boolean {
    if (!hint || OWN_ERROR_TYPES.has(field.type)) return false;
    return !(field.type === 'template' && field.singleLine);
  }

  function blankRequiredTarget(key: FieldKey, required: boolean): boolean {
    if (key !== 'target' || !required) return false;
    return !textOf(key).trim();
  }

  function changeCheck() {
    if (!node) return;
    if (removeCheck) {
      onremove(node.id, true);
      return;
    }
    if (roleLevel) {
      insertCheckBefore(flow, node.id, roleLevel);
      oncheck?.();
    }
  }
</script>

{#if completion && suggestions.length}
  <TokenSuggestions id={uid} anchor={completion?.anchor} items={suggestions} active={activeSuggestion} label={t('flows.board.variables')} onselect={(token) => completion && insertToken(completion.key, token)} />
{/if}

{#if node && spec}
  <aside class="panel" aria-label={active ? t('flows.board.settings', { label: stepLabel(t, spec) }) : undefined} tabindex="-1" bind:this={panel}>
    <header class="head">
      <IconTile style="--tile: var(--bb-space-6); color: {KIND_TINT[spec.kind]}"><Icon name={spec.icon} size={16} /></IconTile>
      <div class="titles">
        <Heading level={2} variant="card" class="inspector-title bb-fit-box"><bb-fit>{stepLabel(t, spec)}</bb-fit></Heading>
        <Text size="xs" tone="muted" class="inspector-sentence bb-fit-box"><bb-fit>{t('flows.board.settingsSentence', { summary: stepSummary(t, spec) })}</bb-fit></Text>
      </div>
      <IconButton label={t('flows.board.closeSettings')} size="sm" onclick={() => (onclose ? onclose() : (selectedId = null))}><Icon name="x" size={15} /></IconButton>
    </header>

    <div class="last-test">
      <div class="last-test-top">
        <Label as="span" class="last-test-label bb-fit-box"><bb-fit>{t('flows.test.lastTest')}</bb-fit></Label>
        <RunOutcome outcome={stepOutcome(lastStep)} />
      </div>
      <Text size="sm" tone="muted-light" class="last-test-text bb-fit-box" role="status">
        <bb-fit mode="block">{lastText}</bb-fit>
      </Text>
      <Button class="last-test-button" variant="ghost" size="sm" onclick={ontest} disabled={!ontest}><bb-fit>{t('flows.test.test')}</bb-fit></Button>
    </div>

    <div class="scroll">
      {#if staticNote}
        <p class="note static-note bb-fit-box"><bb-fit mode="block">{staticNote}</bb-fit></p>
      {/if}
      {#if awaiting}
        <p class="note">{t('flows.setup.stepNote')}</p>
      {/if}
      {#if spec.fields.length}
        <div class="fields bb-field-sheet">
          {#each spec.fields.filter((field) => fieldMounted(spec, node, field)) as field (field.key)}
            {@const disabled = !fieldVisible(spec, node, field, trigger)}
            {@const label = fieldLabel(t, spec, field, node)}
            {@const hint = fieldHint(t, spec, field)}
            {@const issue = issues.find((issue) => issue.nodeId === node.id && issue.fieldKey === field.key) ?? (!disabled ? inlineFieldIssue(node, field) : undefined)}
            {@const error = issue ? fieldIssueMessage(t, issue, node) : undefined}
            {@const required = !disabled && fieldRequired(node, field.key)}
            {@const tag = required ? t('flows.clarity.required') : undefined}
            {@const inNote = sharesNote(field, hint)}
            {@const placeholder = node.config[field.key] === SETUP_URL ? t('flows.setup.addUrl') : fieldPlaceholder(t, spec, field, node)}
            <fieldset class="field bb-field-row" class:bb-field-row--stack={field.type === 'reward'} class:inactive={disabled} {disabled} inert={disabled} name={field.key} bind:this={fieldElements[field.key]} aria-label={label} tabindex="-1">
            {#if field.type === 'color'}
              <Field fit {label} {tag} error={inNote ? undefined : error} for="{uid}-{field.key}">
                <ColorPicker
                  id="{uid}-{field.key}"
                  value={textOf(field.key) || '#ffffff'}
                  onValueChange={(hex) => (node.config[field.key] = hex)}
                />
              </Field>
            {:else if field.type === 'toggle'}
              <span class="bb-field__label"><span class="bb-field__text bb-fit-box"><bb-fit mode="block">{label}</bb-fit></span></span>
              <div class="toggle">
                <Switch
                  {label}
                  checked={Boolean(node.config[field.key] ?? (field.key === 'liveOnly' && field.initial))}
                  onCheckedChange={(v) => setOption(field.key, v)}
                />
              </div>
            {:else if field.type === 'select'}
              <Field fit {label} {tag} error={inNote ? undefined : error} for="{uid}-{field.key}">
                <Select
                  id="{uid}-{field.key}"
                  options={fieldOptions(t, spec, field)}
                  value={selectValue(node, field)}
                  onchange={(e) => setOption(field.key, e.currentTarget.value)}
                />
              </Field>
            {:else if field.type === 'slider'}
              {@const value = Number(node.config[field.key] ?? field.initial ?? field.min)}
              {@const valueText = field.unit ? `${value} ${field.unit}` : String(value)}
              <Field fit {label} {tag} error={inNote ? undefined : error} for="{uid}-{field.key}" errorId="{uid}-{field.key}-error">
                <div class="slider-row">
                  <Slider
                    id="{uid}-{field.key}"
                    min={field.min}
                    max={field.max}
                    step={field.step}
                    {value}
                    aria-valuetext={valueText}
                    aria-invalid={Boolean(error)}
                    aria-describedby={error ? `${uid}-${field.key}-error` : undefined}
                    oninput={(e) => (node.config[field.key] = e.currentTarget.valueAsNumber)}
                  />
                  <span class="unit slider-value bb-fit-box" aria-hidden="true"><bb-fit>{valueText}</bb-fit></span>
                </div>
              </Field>
            {:else if field.type === 'number'}
              {@const unit = field.whenTrigger === 'amount' && amountUnit ? t(`flows.line.timerAmount.${amountUnit}.unit`) : fieldUnit(t, spec, field)}
              <Field fit {label} {tag} error={inNote ? undefined : error} for="{uid}-{field.key}">
                <Input
                  id="{uid}-{field.key}"
                  type="number"
                  min={field.min}
                  max={field.max}
                  {placeholder}
                  step={field.step}
                  invalid={Boolean(error)}
                  value={numberValue(node, field.key)}
                  oninput={(e) => setNumber(field.key, e.currentTarget.value)}
                >
                  {#snippet trailing()}{#if unit}<span class="unit">{unit}</span>{/if}{/snippet}
                </Input>
              </Field>
            {:else if field.type === 'duration'}
              {@const value = durationConfig?.[field.key]}
              {@const outside = durationOutside(field, value)}
              {@const range = durationRange(t, spec, field)}
              <Field fit {label} {tag} hint={outside ? undefined : range} error={error ?? (outside ? range : undefined)} errorId="{uid}-{field.key}-error">
                {#key node.id}
                  <DurationField
                    compact
                    id="{uid}-{field.key}"
                    min={field.min}
                    max={field.max}
                    {label}
                    invalid={outside || Boolean(error)}
                    describedby={outside || error ? `${uid}-${field.key}-error` : undefined}
                    bind:value={
                      () => (value === undefined ? Number.NaN : Number(value)),
                      (seconds) => setDuration(node, field.key, seconds)
                    }
                  />
                {/key}
              </Field>
            {:else if field.type === 'template' && field.singleLine}
              {@const targetInput = field.key === 'target'}
              {@const missingTarget = blankRequiredTarget(field.key, required)}
              {@const shown = disabled ? undefined : error ?? (missingTarget ? t('flows.test.noTarget') : undefined)}
              <Field fit {label} tag={targetFieldFor(spec) ? undefined : tag} error={targetInput ? undefined : shown} for="{uid}-{field.key}" errorId="{uid}-{field.key}-error">
                <Input
                  id="{uid}-{field.key}"
                  required={targetInput && required}
                  aria-invalid={shown ? true : undefined}
                  aria-describedby={shown ? `${uid}-${field.key}-error` : undefined}
                  maxlength={field.max}
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
                {#if targetInput}
                  <small class="bb-field__error bb-fit-box target-error" class:faded={!shown} id="{uid}-{field.key}-error" role="alert"><bb-fit mode="block">{shown ?? ''}</bb-fit></small>
                {/if}
              </Field>
            {:else if field.type === 'template'}
              <Field fit {label} {tag} error={inNote ? undefined : error} for="{uid}-{field.key}">
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
              <ChoiceField {node} {field} {label} {tag} {error} draft={field.source === 'namedCooldowns' ? namedCooldownDrafts : flow.nodes} id="{uid}-{field.key}" />
            {:else if field.type === 'target'}
              <ChoiceField {node} {field} {label} {error} options={targetOptions(t, targetModesFor(flow, node))} id="{uid}-{field.key}" />
            {:else if field.type === 'reward'}
              <RewardField {node} {label} {tag} {error} settledByStep={hasRedemptionStep(flow.nodes)} id="{uid}-{field.key}" />
            {:else if field.type === 'goveeLight'}
              <GoveeLightField {node} {tag} fieldError={error} id="{uid}-{field.key}" onConfig={(config) => (node.config = config)} />
            {:else if field.type === 'login'}
              {#key node.id}
                <UserField {node} id="{uid}-{field.key}" {label} {placeholder} tag={targetFieldFor(spec) ? undefined : tag} error={disabled ? undefined : error} fixedStatus={targetFieldFor(spec)} multiple={node.type === 'cond.user'} />
              {/key}
            {:else}
              <Field fit {label} {tag} error={inNote ? undefined : error} for="{uid}-{field.key}">
                <Input
                  id="{uid}-{field.key}"
                  {placeholder}
                  value={textOf(field.key)}
                  oninput={(e) => (node.config[field.key] = e.currentTarget.value)}
                />
              </Field>
            {/if}
            {#if inNote && error}
              <small class="bb-field__error bb-fit-box field-hint" role="alert" id="{uid}-{field.key}-error"><bb-fit mode="block">{error}</bb-fit></small>
            {:else if hint}
              <small class="bb-field__hint bb-fit-box field-hint"><bb-fit mode="block">{hint}</bb-fit></small>
            {/if}
            {#if node.type === 'reply.http_get' && field.key === 'url'}
              {@const url = textOf(field.key).trim()}
              {#key JSON.stringify([node.id, url])}
                <WebReadTryIt {url} onPick={({ path, sample }) => {
                  node.config.jsonPath = path;
                  onResponseSample?.(node.id, sample);
                }} />
              {/key}
            {/if}
            {#if node.type === 'reply.var' && field.key === 'name'}
              <div class="variable-preview" class:faded={!variableToken} aria-hidden={!variableToken}>
                <VarCard token={variableToken ?? '\u00a0'} title={variableToken} />
              </div>
            {/if}
            </fieldset>
          {/each}
        </div>
      {/if}

      <div class="role-note" class:faded={!roleNote}>
        <p class="note role-warning bb-fit-box" role="status"><bb-fit mode="block">{roleNote ?? ''}</bb-fit></p>
        <div class="check-action" class:faded={!checkAction} inert={!checkAction}>
          <Button variant="ghost" onclick={changeCheck}><bb-fit>{t(removeCheck ? 'flows.step.condition.role.remove' : 'flows.step.requirement.addCheck')}</bb-fit></Button>
        </div>
      </div>
      {#if hasCheckNote(node) || node.type === 'cond.pattern'}
        <p class="note check-note bb-fit-box" class:faded={!passingNote} role="status"><bb-fit mode="block">{passingNote ?? ''}</bb-fit></p>
      {/if}

      {#if branches.length}
        <section class="group">
          <h3 class="group-title">{t('flows.board.branchesTitle')}</h3>
          {#if compareExample}
            <p class="note compare-example bb-fit-box"><bb-fit mode="block">{compareExample}</bb-fit></p>
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

      {#if templateFields.length}
        <section class="group variable-group" class:inactive={!insertKey} inert={!insertKey}>
          <h3 class="group-title">{t('flows.board.variables')}</h3>
          <SearchInput fill bind:value={query} placeholder={t('flows.board.searchVariables')} />
          {#if flowVars.length}
            <h4 class="subgroup-title">{t('flows.board.variablesFlow')}</h4>
            <ul class="vars">
              {#each flowVars as v (v.token)}
                <li><VarCard stacked token={v.label} hint={v.hint} title={hintTitle(v.token, v.hint)} onclick={() => insertKey && insertToken(insertKey, v.token)} /></li>
              {/each}
            </ul>
          {:else if !tokens.length && !needle}
            <p class="note">{t('flows.board.variablesConnect')}</p>
          {/if}
          {#if channelVars.length}
            <h4 class="subgroup-title">{needle ? t('flows.board.variablesAll') : t('flows.board.variablesPopular')}</h4>
            <ul class="vars">
              {#each channelVars as v (v.token)}
                <li><VarCard stacked token={v.label} hint={v.hint} title={hintTitle(v.token, v.hint)} onclick={() => insertKey && insertToken(insertKey, v.token)} /></li>
              {/each}
            </ul>
          {/if}
          {#if noVariableMatches}
            <p class="note">{t('flows.board.variablesNone', { query: query.trim() })}</p>
          {/if}
        </section>
      {/if}
      <FlowRehearsal steps={spec.kind === 'condition' ? [] : rehearsal.steps} focusId={node.id} {summary} />
    </div>

    <footer class="foot">
      <div class="run-step">
        <Switch
          label={t('flows.board.runStep')}
          checked={spec.kind !== 'reply' || node.config.disabled !== true}
          disabled={spec.kind !== 'reply' || awaiting}
          describedby={spec.kind !== 'reply' ? `${uid}-always-runs` : undefined}
          onCheckedChange={(checked) => {
            if (checked) delete node.config.disabled;
            else node.config.disabled = true;
          }}
        />
        <span class="run-label bb-fit-box"><bb-fit mode="block">{t('flows.board.runStep')}</bb-fit></span>
        <span id="{uid}-always-runs" class="run-note bb-fit-box" class:faded={spec.kind === 'reply'} aria-hidden={spec.kind === 'reply'}>
          <bb-fit mode="block">{t('flows.board.checksAlwaysRun')}</bb-fit>
        </span>
      </div>
      <IconButton label={t('flows.board.duplicate')} tooltip size="sm" onclick={duplicate}><UiIcon name="copy" size={15} /></IconButton>
      <IconButton label={t('flows.board.removeStep')} tooltip size="sm" tone="danger" onclick={remove}><UiIcon name="trash" size={15} /></IconButton>
    </footer>
  </aside>
{/if}

<style>
  .panel {
    contain: layout style;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: var(--bb-depth-1), 0 16px 48px rgba(var(--bb-shadow-rgb), 0.55);
    --panel-pad: var(--bb-space-5);
  }
  .head {
    display: flex;
    flex: none;
    align-items: center;
    gap: var(--bb-space-3);
    padding: var(--bb-space-4) var(--bb-space-3) var(--bb-space-4) var(--panel-pad);
    border-block-end: 1px solid var(--bb-rule);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .titles :global(.inspector-title) {
    height: var(--bb-space-5);
    margin: 0;
    font: 600 var(--bb-text-md)/1.25 var(--bb-font-body);
    letter-spacing: normal;
  }
  .titles :global(.inspector-sentence) {
    height: var(--bb-space-4);
    margin: 0;
    line-height: 1.34;
  }
  .last-test {
    display: grid;
    flex: none;
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-rows: var(--bb-space-5) auto;
    column-gap: var(--bb-space-3);
    row-gap: var(--bb-space-1);
    align-items: center;
    margin: var(--bb-space-4) var(--panel-pad) 0;
    padding: var(--bb-space-3);
    border-radius: var(--bb-radius-md);
    background: var(--bb-inset-bg);
    box-shadow: var(--bb-depth-0);
  }
  .last-test-top {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .last-test :global(.last-test-label) {
    min-width: 0;
    line-height: 1.34;
    max-width: var(--bb-space-9);
    height: var(--bb-space-4);
  }
  .last-test :global(.last-test-text) {
    grid-column: 1;
    height: calc(var(--bb-space-5) + var(--bb-space-4));
    margin: 0;
    overflow-wrap: anywhere;
  }
  .last-test :global(.last-test-button) {
    grid-area: 1 / 2 / 3 / 3;
    max-width: var(--bb-space-9);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scrollbar-width: thin;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    padding: var(--bb-space-5) var(--panel-pad);
  }
  .scroll > :global(*) {
    flex: none;
  }
  .fields {
    display: flex;
    flex-direction: column;
    --field-gap: var(--bb-space-2);
  }
  .field,
  .variable-group,
  .target-error { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  .inactive { opacity: 0.45; }
  .target-error { height: var(--bb-space-6); }
  .field-hint { height: var(--bb-space-7); }
  .unit {
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .slider-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) var(--bb-space-7);
    align-items: center;
    gap: var(--bb-space-2);
    min-height: var(--field-control-h);
  }
  .slider-value {
    text-align: end;
    white-space: nowrap;
  }
  .toggle {
    display: flex;
    align-items: center;
    min-height: var(--field-control-h);
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .group-title {
    margin: 0;
    font: 600 var(--bb-text-sm)/var(--bb-space-5) var(--bb-font-body);
    color: var(--bb-white);
  }
  .branches {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .branch {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    font-size: var(--bb-text-sm);
  }
  .branch-name {
    font-weight: 500;
    color: var(--bb-white);
  }
  .branch:first-child .branch-name {
    color: var(--bb-green-glow);
  }
  .branch-count {
    margin-inline-start: auto;
    color: var(--bb-muted-light);
  }
  .subgroup-title {
    margin: var(--bb-space-1) 0 0;
    font: 500 var(--bb-text-sm)/var(--bb-space-5) var(--bb-font-body);
    color: var(--bb-muted-light);
  }
  .vars {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-1);
  }
  .role-note {
    display: grid;
    grid-template-rows: minmax(0, 1fr) var(--bb-space-6);
    gap: var(--bb-space-1);
    height: calc(var(--bb-space-8) + var(--bb-space-4));
  }
  .role-note,
  .variable-preview,
  .check-note,
  .check-action { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  .check-note { height: var(--bb-space-6); }
  .check-action {
    display: flex;
    align-items: center;
    min-width: 0;
  }
  .role-warning { min-width: 0; }
  .static-note,
  .compare-example { height: var(--bb-space-7); }
  .faded { opacity: 0; }
  .note {
    margin: 0;
    font-size: var(--bb-text-sm);
    line-height: 1.45;
    color: var(--bb-muted-light);
  }
  .foot {
    display: flex;
    flex: none;
    align-items: center;
    gap: var(--bb-space-2);
    padding: var(--bb-space-4) var(--bb-space-4) var(--bb-space-4) var(--panel-pad);
    border-block-start: 1px solid var(--bb-rule);
    background: var(--bb-bg-1);
  }
  .run-step {
    flex: 1;
    min-width: 0;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    grid-template-rows: var(--bb-space-5) var(--bb-space-4);
    align-items: center;
    column-gap: var(--bb-space-3);
  }
  .run-step > :global(:first-child) { grid-row: 1 / 3; }
  .run-label,
  .run-note {
    display: grid;
    align-items: center;
    height: 100%;
  }
  .run-label {
    color: var(--bb-white);
    font-size: var(--bb-text-sm);
    font-weight: 500;
  }
  .run-note {
    color: var(--bb-muted);
    font-size: var(--bb-text-xs);
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  @media (max-width: 760px) {
    .panel { --panel-pad: var(--bb-space-4); }
  }
  @media (prefers-reduced-motion: reduce) {
    .field,
    .variable-group,
    .target-error,
    .role-note,
    .variable-preview,
    .check-note,
    .check-action,
    .run-note { transition: none; }
  }
</style>
