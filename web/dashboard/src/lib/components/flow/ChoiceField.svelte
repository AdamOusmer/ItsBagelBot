<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import { page } from '$app/state';
  import { actionPayload, getI18n } from '@bagel/kit';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { unavailable, withDraftNames, withSaved, type ChoiceList, type ChoiceOption, type ChoiceProblem, type ChoiceSource } from '#lib/flow/choices';
  import { targetMode, type FieldSpec, type FlowNode } from '#lib/flow/model';
  import { configText } from '#lib/flow/setup';
  import { isSetupValue } from '@bagel/kit/automations';

  let { node, field, id, label, tag, error, draft = [], options = [] }: {
    node: FlowNode; field: Extract<FieldSpec, { type: 'choice' | 'target' }>; id: string; label: string;
    tag?: string; error?: string; draft?: FlowNode[]; options?: { value: string; label: string }[];
  } = $props();

  const { t } = getI18n();

  let attempt = $state(0);
  let refreshing = $state(false);

  const value = $derived(configText(node, field.key));
  const awaiting = $derived(isSetupValue(node.config[field.key]));
  const nameKey = $derived(field.type === 'choice' ? field.nameKey : undefined);
  const savedName = $derived(nameKey ? configText(node, nameKey) : '');
  const source = $derived(field.type === 'choice' ? field.source : undefined);
  const device = $derived(configText(node, 'device'));
  const sku = $derived(configText(node, 'sku'));

  const isReward = (source: ChoiceSource) => source === 'rewards' || source === 'manageableRewards';

  const chooseText = (source: ChoiceSource) => {
    if (awaiting && isReward(source)) return t('flows.setup.pickReward');
    if (isReward(source)) return t('flows.choose.reward');
    if (source === 'counters') return t('flows.choose.counter');
    if (source === 'namedCooldowns') return t('flows.choose.namedCooldown');
    if (source === 'namedCountdowns') return t('flows.choose.namedCountdown');
    if (source === 'recipes') return t('flows.choose.recipe');
    return t('flows.choose.scene');
  };

  const emptyText = (source: ChoiceSource) => {
    if (source === 'manageableRewards') return t('flows.choiceEmpty.manageableRewards');
    if (source === 'rewards') return t('flows.choiceEmpty.rewards');
    if (source === 'counters') return t('flows.choiceEmpty.counters');
    if (source === 'namedCooldowns') return t('flows.choiceEmpty.namedCooldowns');
    if (source === 'namedCountdowns') return t('flows.choiceEmpty.namedCountdowns');
    if (source === 'recipes') return t('flows.choiceEmpty.recipes');
    return t('flows.choiceEmpty.scenes');
  };

  const failedText = (source: ChoiceSource) => {
    if (isReward(source)) return t('flows.choiceProblem.rewards');
    if (source === 'counters') return t('flows.choiceProblem.counters');
    if (source === 'namedCooldowns') return t('flows.choiceProblem.namedCooldowns');
    if (source === 'namedCountdowns') return t('flows.choiceProblem.namedCountdowns');
    if (source === 'recipes') return t('flows.choiceProblem.recipes');
    return t('flows.choiceProblem.scenes');
  };

  const problemText = (source: ChoiceSource, problem: ChoiceProblem) => {
    if (problem === 'keyRejected') return t('flows.choiceProblem.keyRejected');
    return failedText(source);
  };

  const missingText = (source: ChoiceSource, name: string) =>
    (source === 'namedCooldowns' || source === 'namedCountdowns') ? t('flows.choiceUnowned', { name }) : t('flows.choiceMissing', { name });

  async function fetchScenes(light: string, model: string): Promise<ChoiceList> {
    const body = new FormData();
    body.set('device', light);
    body.set('sku', model);
    try {
      const res = await fetch('?/scenes', { method: 'POST', body, headers: { 'x-sveltekit-action': 'true' } });
      return actionPayload<{ list?: ChoiceList }>(deserialize(await res.text()))?.list ?? unavailable();
    } catch {
      return unavailable();
    }
  }

  const list = $derived.by((): Promise<ChoiceList> | null => {
    void attempt;
    if (!source) return null;
    if (source !== 'scenes') return page.data[source] as Promise<ChoiceList>;
    return device && sku ? fetchScenes(device, sku) : null;
  });

  async function retry() {
    refreshing = true;
    try {
      if (source === 'scenes') attempt++;
      else await invalidate('flows:choices');
    } finally {
      refreshing = false;
    }
  }

  function choose(options: ChoiceOption[], next: string) {
    const picked = options.find((option) => option.value === next);
    node.config[field.key] = next;
    if (nameKey) node.config[nameKey] = picked?.label ?? '';
    Object.assign(node.config, picked?.meta);
  }
</script>

{#if field.type === 'target'}
  <Field fit {label} {error} errorId="{id}-error">
    <SegmentedControl {id} {label} {options} class="bb-tabs--even bb-tabs--plain" value={targetMode(node.config)}
      aria-invalid={Boolean(error)} aria-describedby={error ? `${id}-error` : undefined}
      onValueChange={(next) => (node.config.who = next)} />
  </Field>
{:else}
<div class="choice-field bb-field-span">
<Field fit {label} {tag} {error} errorId="{id}-error" for={id}>
  <div class="choice-content">
  {#if list === null}
    <Text size="sm" tone="muted">{t('flows.choose.light')}</Text>
    <Select {id} options={value ? [{ value, label: savedName || value }] : []} {value} disabled placeholder={chooseText(field.source)} />
  {:else}
    {#await list}
      <div class="loading"><Spinner /><Text size="sm" tone="muted">{t('flows.choiceLoading')}</Text></div>
    {:then loaded}
      {@const result = withDraftNames(loaded, field.source, draft)}
      {@const options = withSaved(result, value, savedName)}
      <Select {id} {options} {value} disabled={!options.length} aria-invalid={Boolean(error)} aria-describedby={error ? `${id}-error` : undefined} placeholder={chooseText(field.source)} onchange={(e) => choose(options, e.currentTarget.value)} />
    {/await}
  {/if}
  </div>
</Field>
{#if list !== null}
  {#await list then loaded}
    {@const result = withDraftNames(loaded, field.source, draft)}
    <div class="row">
      {#if result.problem}
        <Text size="sm" tone="muted">{problemText(field.source, result.problem)}</Text>
        {#if result.problem !== 'keyRejected'}
          <button class="refresh" type="button" disabled={refreshing} onclick={retry}>{t('flows.choiceRetry')}</button>
        {/if}
      {:else if result.options.length === 0}
        <Text size="sm" tone="muted">{emptyText(field.source)}</Text>
        <button class="refresh" type="button" disabled={refreshing} onclick={retry}>{t('flows.choiceRetry')}</button>
      {:else if value && !result.options.some((option) => option.value === value)}
        <Text size="sm" tone="muted">{missingText(field.source, savedName || value)}</Text>
      {/if}
        </div>
  {/await}
{/if}
</div>
{/if}

<style>
  .choice-field { min-height: 114px; }
  .choice-content { display: flex; flex-direction: column; gap: 8px; }
  .row, .loading {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .row { min-height: 40px; align-items: baseline; }
  .loading { height: 40px; }
  .refresh { flex: none; padding: 0; border: 0; background: none; color: var(--bb-white); font: inherit; font-size: 13px; text-decoration: underline; text-underline-offset: 3px; cursor: pointer; }
  .refresh:disabled { opacity: 0.5; cursor: wait; }
</style>
