<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n } from '@bagel/kit';
  import { postAction, reseedOnChange } from '#lib/client-actions.svelte';
  import type { RewardReply } from '#lib/flow/replies';
  import RewardCoreFields from '#lib/components/channelpoints/RewardCoreFields.svelte';
  import { LINKED_REWARD_CHOICE, type FlowNode } from '#lib/flow/model';
  import { configText } from '#lib/flow/setup';
  import { REWARD_COOLDOWN_MAX, REWARD_COST_MAX, REWARD_COST_MIN, wholeBetween, type RewardCoreDraft } from '#lib/reward-fields';
  import ChoiceField from './ChoiceField.svelte';

  let { node, id, label }: { node: FlowNode; id: string; label: string } = $props();

  const { t } = getI18n();

  const wholeNumber = (value: unknown) => (Number.isFinite(Number(value)) ? Number(value) : 0);

  const draft: RewardCoreDraft = {
    get title() {
      return configText(node, 'title');
    },
    set title(value) {
      node.config.title = value;
    },
    get cost() {
      return node.config.cost === undefined ? Number.NaN : Number(node.config.cost);
    },
    set cost(value) {
      node.config.cost = wholeNumber(value);
    },
    get color() {
      return configText(node, 'color');
    },
    set color(value) {
      node.config.color = value;
    },
    get cooldown() {
      return Number(node.config.cooldown ?? 0);
    },
    set cooldown(value) {
      node.config.cooldown = wholeNumber(value);
    },
    get onRedeem() {
      return configText(node, 'onRedeem') || 'fulfill';
    },
    set onRedeem(value) {
      node.config.onRedeem = value;
    },
    replyMessage: ''
  };

  const afterOptions = [
    { value: 'fulfill', label: t('flows.reward.afterFulfill') },
    { value: 'cancel', label: t('flows.reward.afterCancel') },
    { value: 'leave', label: t('flows.reward.afterLeave') }
  ];

  type Mode = 'existing' | 'create';
  type Source = { title: string; reward: string; rewardTitle: string };

  const modeOptions = [
    { value: 'existing', label: t('flows.reward.modeExisting') },
    { value: 'create', label: t('flows.reward.modeCreate') }
  ];

  let picked = $state<Mode | null>(null);
  let busy = $state(false);
  const parked: Partial<Record<Mode, Source>> = {};
  const blank: Source = { title: '', reward: '', rewardTitle: '' };

  reseedOnChange(() => node.id, () => (picked = null));

  const mode = $derived<Mode>(picked ?? (draft.title !== '' ? 'create' : 'existing'));
  const rewardId = $derived(configText(node, 'reward'));
  const created = $derived(mode === 'create' && rewardId !== '');
  const errors = $derived({
    cost: wholeBetween(draft.cost, REWARD_COST_MIN, REWARD_COST_MAX) ? undefined : t('flows.reward.errCost'),
    cooldown: wholeBetween(draft.cooldown, 0, REWARD_COOLDOWN_MAX) ? undefined : t('flows.reward.errCooldown')
  });
  const ready = $derived(draft.title.trim() !== '' && !errors.cost && !errors.cooldown);

  function chooseMode(next: string) {
    if (next === mode) return;
    parked[mode] = { title: draft.title, reward: rewardId, rewardTitle: configText(node, 'rewardTitle') };
    const source = parked[next as Mode] ?? blank;
    node.config.title = source.title;
    node.config.reward = source.reward;
    node.config.rewardTitle = source.rewardTitle;
    picked = next as Mode;
  }

  function adopt(reply: Extract<RewardReply, { ok: true }>) {
    node.config.reward = reply.rewardId;
    node.config.rewardTitle = reply.title;
    node.config.title = reply.title;
    node.config.cost = reply.cost;
    node.config.color = reply.color;
    node.config.cooldown = reply.cooldown;
  }

  async function submit() {
    if (busy || !ready) return;
    busy = true;
    const body = new FormData();
    body.set('payload', JSON.stringify({ rewardId, title: draft.title, cost: draft.cost, color: draft.color, cooldown: draft.cooldown, inputRequired: node.config.userInput === true }));
    const reply = await postAction<RewardReply>('reward', body);
    busy = false;
    if (!reply) toast('danger', t('flows.reward.requestFailed'));
    else if (!reply.ok) toast('danger', reply.error);
    else {
      const wasCreated = created;
      adopt(reply);
      toast('success', t(wasCreated ? 'flows.reward.updated' : 'flows.reward.created'));
      await invalidate('flows:choices');
    }
  }
</script>

<SegmentedControl options={modeOptions} value={mode} onValueChange={chooseMode} label={t('flows.reward.modeLabel')} />

<div class="stack">
  <div class="pane" class:hidden={mode !== 'existing'} inert={mode !== 'existing'}>
    <ChoiceField {node} field={LINKED_REWARD_CHOICE} {label} {id} />
  </div>
  <div class="pane" class:hidden={mode !== 'create'} inert={mode !== 'create'}>
    <RewardCoreFields
      {draft}
      {errors}
      idPrefix="flow-reward"
      withReply={false}
      {afterOptions}
      labels={{
        title: t('flows.reward.fieldTitle'),
        cost: t('flows.reward.fieldCost'),
        color: t('flows.reward.fieldColor'),
        cooldown: t('flows.reward.fieldCooldown'),
        cooldownTag: t('flows.reward.fieldCooldownTag'),
        after: t('flows.reward.afterTitle')
      }}
    />
    <SwitchRow
      control="end"
      checked={node.config.userInput === true}
      onCheckedChange={(checked) => (node.config.userInput = checked)}
      label={t('flows.reward.inputLabel')}
      hint={t('flows.reward.inputHint')}
      hintId="{id}-input-desc"
    />
    <Button variant="primary" {busy} disabled={!ready} onclick={submit}>{created ? t('flows.reward.update') : t('flows.reward.create')}</Button>
  </div>
</div>

<style>
  .stack {
    display: grid;
  }
  .pane {
    grid-area: 1 / 1;
    display: flex;
    flex-direction: column;
    gap: 12px;
    align-self: start;
    transition: opacity 0.2s ease;
  }
  .pane.hidden {
    opacity: 0;
    visibility: hidden;
    pointer-events: none;
  }
</style>
