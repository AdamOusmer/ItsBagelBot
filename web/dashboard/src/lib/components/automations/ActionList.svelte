<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import { ACTION_TYPES, CONDITION_TYPES, MAX_ACTIONS, type Action, type Branch, type Condition } from '@bagel/kit/automations';
  import type { VariableSurface } from '@bagel/kit/variables';
  import ActionList from './ActionList.svelte';
  import TypeFields from './TypeFields.svelte';
  import TypedNodeEditor from './TypedNodeEditor.svelte';
  import { actionDef, issueExact, issueAt, makeAction, makeCondition, movedWithin, type IssueIndex } from './model';

  const MAX_BRANCHES = 5;
  const MIN_BRANCHES = 2;
  const NESTING_TYPES = new Set(['if', 'random_pick']);

  let {
    actions = $bindable(),
    path,
    depth = 0,
    issues,
    surface,
    others = []
  }: {
    actions: Action[];
    path: string;
    depth?: number;
    issues: IssueIndex;
    surface: VariableSurface;
    others?: { id: string; name: string }[];
  } = $props();

  const { t } = getI18n();

  let addType = $state('chat');

  const typeOptions = $derived(
    ACTION_TYPES.filter((d) => depth === 0 || !NESTING_TYPES.has(d.type)).map((d) => ({
      value: d.type,
      label: d.supported ? t(`automations.action.${d.type}` as MessageKey) : `${t(`automations.action.${d.type}` as MessageKey)} (${t('automations.comingSoon')})`,
      disabled: !d.supported
    }))
  );
  const runTargets = $derived(others.map((o) => ({ value: o.id, label: o.name })));
  const listCode = $derived(issueExact(issues, path));

  const at = (i: number) => `${path}[${i}]`;
  const listOf = (action: Action, key: string): Action[] => (action.params?.[key] as Action[]) ?? [];
  const branchesOf = (action: Action): Branch[] => (action.params?.branches as Branch[]) ?? [];

  function add() {
    actions = [...actions, makeAction(addType)];
  }

  function remove(i: number) {
    actions = actions.filter((_, j) => j !== i);
  }

  function move(i: number, to: number) {
    actions = movedWithin(actions, i, to);
  }

  function changeType(action: Action, type: string) {
    const fresh = makeAction(type);
    action.type = type;
    action.params = fresh.params;
  }

  function addBranch(action: Action) {
    action.params!.branches = [...branchesOf(action), { weight: 1, actions: [] }];
  }

  function removeBranch(action: Action, b: number) {
    action.params!.branches = branchesOf(action).filter((_, j) => j !== b);
  }

  function setWeight(branch: Branch, e: Event) {
    const n = (e.currentTarget as HTMLInputElement).valueAsNumber;
    branch.weight = Number.isNaN(n) ? (null as unknown as number) : n;
  }
</script>

<div class="al" data-depth={depth}>
  {#each actions as action, i (action)}
    {@const def = actionDef(action.type)}
    {@const typeCode = issueAt(issues, `${at(i)}.type`)}
    <div class="ar" class:off={action.enabled === false}>
      <div class="ar-head">
        <span class="ar-idx"><Text as="span" size="xs" mono tone="muted">{String(i + 1).padStart(2, '0')}</Text></span>
        <div class="ar-type">
          <Select fill label={t('automations.fieldType')} options={typeOptions} invalid={!!typeCode} value={action.type} onchange={(e) => changeType(action, e.currentTarget.value)} />
        </div>
        <span class="ar-note">
          {#if typeCode}
            <Text as="span" size="xs" tone="danger">{t(`automations.issue.${typeCode}` as MessageKey)}</Text>
          {:else if def && !def.supported}
            <Text as="span" size="xs" tone="warning">{t('automations.comingSoonNote')}</Text>
          {/if}
        </span>
        <span class="ar-tools">
          <Switch checked={action.enabled !== false} label={t('automations.actionEnabled', { n: String(i + 1) })} onCheckedChange={(v) => (action.enabled = v)} />
          <IconButton size="sm" label={t('automations.moveUp', { n: String(i + 1) })} disabled={i === 0} onclick={() => move(i, i - 1)}>
            <span class="flip"><Icon name="chevron" size={15} /></span>
          </IconButton>
          <IconButton size="sm" label={t('automations.moveDown', { n: String(i + 1) })} disabled={i === actions.length - 1} onclick={() => move(i, i + 1)}>
            <Icon name="chevron" size={15} />
          </IconButton>
          <IconButton size="sm" tone="danger" label={t('automations.removeAction', { n: String(i + 1) })} onclick={() => remove(i)}>
            <Icon name="trash" size={15} />
          </IconButton>
        </span>
      </div>

      {#if def && action.params}
        <TypeFields {def} bind:params={action.params} path={`${at(i)}.params`} {issues} {surface} {runTargets} disabled={!def.supported} />
      {/if}

      {#if action.type === 'if' && depth === 0 && action.params}
        <div class="nest">
          <Text as="span" size="xs" tone="muted">{t('automations.ifCondition')}</Text>
          <TypedNodeEditor bind:node={() => action.params!.condition as Condition, (v) => (action.params!.condition = v)} defs={CONDITION_TYPES} make={makeCondition} path={`${at(i)}.params.condition`} {issues} {surface} />
          <Text as="span" size="xs" tone="muted">{t('automations.ifThen')}</Text>
          <ActionList bind:actions={() => listOf(action, 'then'), (v) => (action.params!.then = v)} path={`${at(i)}.params.then`} depth={1} {issues} {surface} {others} />
          <Text as="span" size="xs" tone="muted">{t('automations.ifElse')}</Text>
          <ActionList bind:actions={() => listOf(action, 'else'), (v) => (action.params!.else = v)} path={`${at(i)}.params.else`} depth={1} {issues} {surface} {others} />
        </div>
      {:else if action.type === 'random_pick' && depth === 0 && action.params}
        <div class="nest">
          {#each branchesOf(action) as branch, b (branch)}
            {@const weightCode = issueAt(issues, `${at(i)}.params.branches[${b}].weight`)}
            <div class="br-head">
              <Text as="span" size="xs" tone="muted">{t('automations.branchN', { n: String(b + 1) })}</Text>
              <div class="br-weight">
                <Input fill mono type="number" min={1} max={100} aria-label={t('automations.fieldWeight')} invalid={!!weightCode} value={branch.weight} oninput={(e: Event) => setWeight(branch, e)} />
              </div>
              <span class="ar-note">{#if weightCode}<Text as="span" size="xs" tone="danger">{t(`automations.issue.${weightCode}` as MessageKey)}</Text>{/if}</span>
              <IconButton size="sm" tone="danger" label={t('automations.removeBranch', { n: String(b + 1) })} disabled={branchesOf(action).length <= MIN_BRANCHES} onclick={() => removeBranch(action, b)}>
                <Icon name="trash" size={15} />
              </IconButton>
            </div>
            <ActionList bind:actions={() => branch.actions ?? [], (v) => (branch.actions = v)} path={`${at(i)}.params.branches[${b}].actions`} depth={1} {issues} {surface} {others} />
          {/each}
          <div class="br-add">
            <Button variant="add" disabled={branchesOf(action).length >= MAX_BRANCHES} onclick={() => addBranch(action)}>+ {t('automations.addBranch')}</Button>
            <span class="ar-note">{#if issueExact(issues, `${at(i)}.params.branches`)}<Text as="span" size="xs" tone="danger">{t(`automations.issue.${issueExact(issues, `${at(i)}.params.branches`)}` as MessageKey)}</Text>{/if}</span>
          </div>
        </div>
      {/if}
    </div>
  {/each}

  <div class="al-add">
    <div class="al-add-type"><Select fill label={t('automations.addActionType')} options={typeOptions} bind:value={addType} /></div>
    <Button variant="add" disabled={depth === 0 && actions.length >= MAX_ACTIONS} onclick={add}>+ {t('automations.addAction')}</Button>
    <span class="ar-note">{#if listCode}<Text as="span" size="xs" tone="danger">{t(`automations.issue.${listCode}` as MessageKey)}</Text>{/if}</span>
  </div>
</div>

<style>
  .al { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .ar { border: 1px solid var(--bb-glass-border); border-radius: var(--bb-radius-md); padding: 10px 12px 8px; display: flex; flex-direction: column; gap: 8px; }
  .ar.off { opacity: 0.6; }
  .ar-head { display: grid; grid-template-columns: 28px 260px minmax(0, 1fr) auto; align-items: center; column-gap: 12px; min-height: 40px; }
  .ar-type { min-width: 0; }
  .ar-note { display: block; min-width: 0; min-height: 18px; line-height: 18px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .ar-tools { display: inline-flex; align-items: center; gap: 6px; justify-content: flex-end; width: 176px; }
  .flip { display: inline-flex; transform: rotate(180deg); }
  .nest { display: flex; flex-direction: column; gap: 8px; padding-left: 16px; border-left: 1px solid var(--bb-glass-border); }
  .br-head { display: grid; grid-template-columns: 84px 120px minmax(0, 1fr) 32px; align-items: center; column-gap: 12px; min-height: 40px; }
  .br-add { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; column-gap: 12px; min-height: 40px; }
  .al-add { display: grid; grid-template-columns: 260px auto minmax(0, 1fr); align-items: center; column-gap: 12px; min-height: 40px; }
  .al-add-type { min-width: 0; }
</style>
