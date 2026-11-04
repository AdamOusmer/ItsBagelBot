<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { goto, invalidateAll } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { focusFirstInvalid } from '@bagel/ui/svelte/forms';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, getI18n, type MessageKey } from '@bagel/kit';
  import {
    CONDITION_TYPES,
    MAX_CONDITIONS,
    TRIGGER_TYPES,
    validateDefinition,
    validateName,
    type Automation,
    type Issue
  } from '@bagel/kit/automations';
  import type { VariableSurface } from '@bagel/kit/variables';
  import ActionList from './ActionList.svelte';
  import TypedNodeEditor from './TypedNodeEditor.svelte';
  import {
    blankDefinition,
    issueAt,
    issueIndex,
    makeCondition,
    makeTrigger,
    normalizeDefinition
  } from './model';

  type SaveResult = { ok?: boolean; id?: string; code?: string; conflict?: boolean; issues?: Issue[] };

  let {
    automation,
    others,
    usable
  }: {
    automation: Automation | null;
    others: { id: string; name: string }[];
    usable: boolean;
  } = $props();

  const { t } = getI18n();

  const creating = $derived(automation === null);

  // svelte-ignore state_referenced_locally
  let name = $state(automation?.name ?? '');
  // svelte-ignore state_referenced_locally
  let enabled = $state(automation?.enabled ?? true);
  // svelte-ignore state_referenced_locally
  let definition = $state(normalizeDefinition(automation?.definition ?? blankDefinition()));
  // svelte-ignore state_referenced_locally
  let revision = $state<number | undefined>(automation?.revision);
  let addCondition = $state(CONDITION_TYPES[0].type);
  let attempted = $state(false);
  let status = $state<'idle' | 'saving' | 'saved' | 'error' | 'conflict'>('idle');
  let rejected = $state<{ key: string; issues: Issue[] } | null>(null);
  let formEl = $state<HTMLFormElement | null>(null);

  const definitionJson = $derived(JSON.stringify($state.snapshot(definition)));
  const editKey = $derived(`${name}\u0000${enabled}\u0000${definitionJson}`);
  // svelte-ignore state_referenced_locally
  let baseline = $state(editKey);
  const dirty = $derived(editKey !== baseline);

  const serverIssues = $derived(rejected && rejected.key === editKey ? rejected.issues : []);
  const clientIssues = $derived([...validateName(name), ...validateDefinition($state.snapshot(definition))]);
  const showIssues = $derived(attempted || !creating);
  const issues = $derived(issueIndex(showIssues ? [...clientIssues, ...serverIssues] : serverIssues));
  const nameCode = $derived(issueAt(issues, 'name'));
  const surface = $derived<VariableSurface>(
    definition.trigger.type === 'event' && typeof definition.trigger.params?.kind === 'string'
      ? { automation: definition.trigger.params.kind }
      : 'automation'
  );

  const discard = createDiscardGuard(
    () => dirty,
    () => void goto('/automations')
  );

  function reseed() {
    if (!automation) return;
    name = automation.name;
    enabled = automation.enabled;
    definition = normalizeDefinition(automation.definition);
    revision = automation.revision;
    baseline = editKey;
  }

  // svelte-ignore state_referenced_locally
  let seedRevision = automation?.revision;
  $effect(() => {
    const next = automation?.revision;
    if (next === seedRevision) return;
    seedRevision = next;
    reseed();
  });

  function addConditionRow() {
    definition.conditions = [...(definition.conditions ?? []), makeCondition(addCondition)];
  }

  function removeCondition(i: number) {
    definition.conditions = (definition.conditions ?? []).filter((_, j) => j !== i);
  }

  function failureText(payload: SaveResult | undefined): string {
    const code = payload?.conflict ? 'conflict' : (payload?.code ?? 'failed');
    return t(`automations.err.${code}` as MessageKey);
  }

  async function saved(payload: SaveResult) {
    status = 'saved';
    toast('success', t(creating ? 'automations.toastCreated' : 'automations.toastSaved'));
    baseline = editKey;
    if (creating && payload.id) await goto(`/automations/${payload.id}`, { replaceState: true, invalidateAll: true });
    else await invalidateAll();
  }

  function rejectedBy(payload: SaveResult | undefined) {
    status = payload?.conflict ? 'conflict' : 'error';
    rejected = { key: editKey, issues: payload?.issues ?? [] };
    if (!payload?.conflict) toast('danger', failureText(payload));
  }

  const submit: SubmitFunction = ({ cancel }) => {
    if (clientIssues.length > 0) {
      attempted = true;
      cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    status = 'saving';
    return async ({ result }) => {
      const payload = actionPayload<SaveResult>(result);
      if (result.type === 'success' && payload?.ok) await saved(payload);
      else rejectedBy(payload);
    };
  };

  async function reload() {
    status = 'idle';
    rejected = null;
    await invalidateAll();
    reseed();
  }
</script>

<form method="POST" action="?/save" novalidate use:enhance={submit} bind:this={formEl} class="ed">
  <input type="hidden" name="id" value={automation?.id ?? ''} />
  <input type="hidden" name="name" value={name} />
  <input type="hidden" name="revision" value={revision ?? ''} />
  <input type="hidden" name="enabled" value={enabled ? 'on' : ''} />
  <input type="hidden" name="definition" value={definitionJson} />

  <fieldset class="ed-set" disabled={!usable}>
    <section class="sec">
      <div class="sec-title"><Text as="span" size="sm">{t('automations.secBasics')}</Text></div>
      <div class="basics">
        <Field label={t('automations.fieldName')}>
          <Input fill bind:value={name} invalid={!!nameCode} placeholder={t('automations.namePh')} maxlength={256} />
        </Field>
        <div class="basics-on"><SwitchRow control="end" bind:checked={enabled} label={t('automations.fieldEnabled')} hint={t('automations.fieldEnabledHint')} /></div>
      </div>
      <span class="slot"><Text as="span" size="xs" tone="danger">{nameCode ? t(`automations.issue.${nameCode}` as MessageKey) : ''}</Text></span>
    </section>

    <section class="sec">
      <div class="sec-title"><Text as="span" size="sm">{t('automations.secTrigger')}</Text></div>
      <div class="box">
        <TypedNodeEditor
          bind:node={definition.trigger}
          defs={TRIGGER_TYPES}
          make={makeTrigger}
          path="trigger"
          {issues}
          {surface}
        />
      </div>
    </section>

    <section class="sec">
      <div class="sec-title"><Text as="span" size="sm">{t('automations.secConditions')}</Text><Text as="span" size="xs" tone="muted">{t('automations.secConditionsHint')}</Text></div>
      {#each definition.conditions ?? [] as condition, i (condition)}
        <div class="box">
          <TypedNodeEditor
            bind:node={definition.conditions![i]}
            defs={CONDITION_TYPES}
            make={makeCondition}
            path={`conditions[${i}]`}
            {issues}
            {surface}
            removeLabel={t('automations.removeCondition', { n: String(i + 1) })}
            onRemove={() => removeCondition(i)}
          />
        </div>
      {/each}
      <div class="add-row">
        <div class="add-type"><Select fill label={t('automations.addConditionType')} options={CONDITION_TYPES.map((d) => ({ value: d.type, label: t(`automations.condition.${d.type}` as MessageKey) }))} bind:value={addCondition} /></div>
        <Button variant="add" disabled={(definition.conditions?.length ?? 0) >= MAX_CONDITIONS} onclick={addConditionRow}>+ {t('automations.addCondition')}</Button>
        <span class="slot"><Text as="span" size="xs" tone="danger">{issueAt(issues, 'conditions') ? t(`automations.issue.${issueAt(issues, 'conditions')}` as MessageKey) : ''}</Text></span>
      </div>
    </section>

    <section class="sec">
      <div class="sec-title"><Text as="span" size="sm">{t('automations.secActions')}</Text><Text as="span" size="xs" tone="muted">{t('automations.secActionsHint')}</Text></div>
      <ActionList bind:actions={() => definition.actions ?? [], (v) => (definition.actions = v)} path="actions" {issues} {surface} {others} />
    </section>
  </fieldset>

  <div class="notice" aria-live="polite">
    {#if status === 'conflict'}
      <Text as="span" size="sm" tone="danger">{t('automations.err.conflict')}</Text>
      <Button size="sm" variant="secondary" onclick={reload}>{t('automations.reload')}</Button>
    {/if}
  </div>

  <EditorFooter
    {status}
    {dirty}
    canSave={usable && (dirty || creating)}
    saveLabel={creating ? t('automations.create') : t('automations.save')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('automations.saving')}
    savedLabel={t('automations.saved')}
    errorLabel={t('automations.saveFailed')}
    dirtyLabel={t('automations.unsaved')}
    onCancel={() => discard.guard(() => void goto('/automations'))}
  />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('automations.discardTitle')}
  body={t('automations.discardBody')}
  confirmLabel={t('automations.discard')}
  cancelLabel={t('automations.keepEditing')}
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
  tone="danger"
/>

<style>
  .ed { display: flex; flex-direction: column; gap: 20px; }
  .ed-set { border: 0; margin: 0; padding: 0; min-width: 0; display: flex; flex-direction: column; gap: 20px; }
  .sec { display: flex; flex-direction: column; gap: 8px; }
  .sec-title { display: flex; align-items: baseline; gap: 12px; min-height: 24px; }
  .box { border: 1px solid var(--bb-glass-border); border-radius: var(--bb-radius-md); padding: 10px 12px 8px; }
  .basics { display: grid; grid-template-columns: minmax(0, 1fr) 320px; column-gap: 16px; align-items: start; }
  .basics-on { padding-top: 22px; }
  .slot { display: block; min-height: 18px; line-height: 18px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .add-row { display: grid; grid-template-columns: 260px auto minmax(0, 1fr); align-items: center; column-gap: 12px; min-height: 40px; }
  .add-type { min-width: 0; }
  .notice { display: flex; align-items: center; gap: 12px; min-height: 40px; }
</style>
