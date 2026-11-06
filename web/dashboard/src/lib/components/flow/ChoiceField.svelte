<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import { page } from '$app/state';
  import { actionPayload, getI18n } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { unavailable, withSaved, type ChoiceList, type ChoiceOption, type ChoiceProblem, type ChoiceSource } from '#lib/flow/choices';
  import type { FieldSpec, FlowNode } from '#lib/flow/model';
  import { configText } from '#lib/flow/setup';
  import { isSetupValue } from '@bagel/kit/automations';

  let { node, field, id, label }: { node: FlowNode; field: Extract<FieldSpec, { type: 'choice' }>; id: string; label: string } = $props();

  const { t } = getI18n();

  let attempt = $state(0);
  let refreshing = $state(false);

  const value = $derived(configText(node, field.key));
  const awaiting = $derived(isSetupValue(node.config[field.key]));
  const savedName = $derived(field.nameKey ? configText(node, field.nameKey) : '');
  const device = $derived(configText(node, 'device'));
  const sku = $derived(configText(node, 'sku'));

  const isReward = (source: ChoiceSource) => source === 'rewards' || source === 'manageableRewards';

  const chooseText = (source: ChoiceSource) => {
    if (awaiting && isReward(source)) return t('flows.setup.pickReward');
    if (isReward(source)) return t('flows.choose.reward');
    return source === 'counters' ? t('flows.choose.counter') : t('flows.choose.scene');
  };

  const emptyText = (source: ChoiceSource) => {
    if (source === 'manageableRewards') return t('flows.choiceEmpty.manageableRewards');
    if (source === 'rewards') return t('flows.choiceEmpty.rewards');
    return source === 'counters' ? t('flows.choiceEmpty.counters') : t('flows.choiceEmpty.scenes');
  };

  const failedText = (source: ChoiceSource) => {
    if (isReward(source)) return t('flows.choiceProblem.rewards');
    return source === 'counters' ? t('flows.choiceProblem.counters') : t('flows.choiceProblem.scenes');
  };

  const problemText = (source: ChoiceSource, problem: ChoiceProblem) => {
    if (problem === 'keyRejected') return t('flows.choiceProblem.keyRejected');
    return failedText(source);
  };

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
    if (field.source !== 'scenes') return page.data[field.source] as Promise<ChoiceList>;
    return device && sku ? fetchScenes(device, sku) : null;
  });

  async function retry() {
    refreshing = true;
    try {
      if (field.source === 'scenes') attempt++;
      else await invalidate('flows:choices');
    } finally {
      refreshing = false;
    }
  }

  function choose(options: ChoiceOption[], next: string) {
    const picked = options.find((option) => option.value === next);
    node.config[field.key] = next;
    if (field.nameKey) node.config[field.nameKey] = picked?.label ?? '';
    Object.assign(node.config, picked?.meta);
  }
</script>

<Field {label} for={id}>
  {#if list === null}
    <Text size="sm" tone="muted">{t('flows.choose.light')}</Text>
    {#if value}<Select {id} options={[{ value, label: savedName || value }]} {value} disabled />{/if}
  {:else}
    {#await list}
      <div class="row"><Spinner /><Text size="sm" tone="muted">{t('flows.choiceLoading')}</Text></div>
    {:then result}
      {@const options = withSaved(result, value, savedName)}
      {#if options.length}
        <Select {id} {options} {value} placeholder={chooseText(field.source)} onchange={(e) => choose(options, e.currentTarget.value)} />
      {/if}
      {#if result.problem}
        <Text size="sm" tone="muted">{problemText(field.source, result.problem)}</Text>
        {#if result.problem !== 'keyRejected'}
          <Button variant="ghost" busy={refreshing} onclick={retry}>{t('flows.choiceRetry')}</Button>
        {/if}
      {:else if result.options.length === 0}
        <Text size="sm" tone="muted">{emptyText(field.source)}</Text>
      {:else if value && !result.options.some((option) => option.value === value)}
        <Text size="sm" tone="muted">{t('flows.choiceMissing', { name: savedName || value })}</Text>
      {/if}
    {/await}
  {/if}
</Field>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
</style>
