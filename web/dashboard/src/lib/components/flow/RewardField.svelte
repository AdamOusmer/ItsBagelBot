<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import { getI18n } from '@bagel/kit';
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

  const managed = $derived(draft.title !== '');
  const errors = $derived({
    cost: managed && !wholeBetween(draft.cost, REWARD_COST_MIN, REWARD_COST_MAX) ? t('flows.reward.errCost') : undefined,
    cooldown: managed && !wholeBetween(draft.cooldown, 0, REWARD_COOLDOWN_MAX) ? t('flows.reward.errCooldown') : undefined
  });
</script>

<div class="linked" class:managed inert={managed}>
  <ChoiceField {node} field={LINKED_REWARD_CHOICE} {label} {id} />
</div>

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

<style>
  .linked {
    transition: opacity 0.2s ease;
  }
  .linked.managed {
    opacity: 0.45;
  }
</style>
