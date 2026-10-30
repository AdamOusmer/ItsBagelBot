<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import { getI18n } from '@bagel/kit';
  import type { VariableSurface } from '@bagel/kit/variables';
  import ResponseEditor from '#lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '#lib/components/commands/ChatPreview.svelte';
  import DurationField from '#lib/components/shared/DurationField.svelte';
  import { REWARD_COOLDOWN_MAX, REWARD_COST_MAX, type RewardCoreDraft, type RewardCoreErrors } from '#lib/reward-fields';

  let {
    draft = $bindable<RewardCoreDraft>(),
    errors,
    idPrefix,
    labels,
    afterOptions,
    surface,
    defaultReply,
    replySamples,
    previewTag,
    cursorColor = false
  }: {
    draft: RewardCoreDraft;
    errors: RewardCoreErrors;
    idPrefix: string;
    labels: { title: string; cost: string; color: string; cooldown: string; cooldownTag: string; reply: string; after: string };
    afterOptions: { value: string; label: string }[];
    surface: VariableSurface;
    defaultReply: string;
    replySamples: Record<string, string>;
    previewTag: string;
    cursorColor?: boolean;
  } = $props();

  const { t } = getI18n();
</script>

<Field label={labels.title} error={errors.title} errorId="{idPrefix}-title-err">
  <Input
    fill
    invalid={!!errors.title}
    type="text"
    maxlength={45}
    bind:value={draft.title}
    aria-invalid={errors.title ? 'true' : undefined}
    aria-describedby={errors.title ? `${idPrefix}-title-err` : undefined}
    required
  />
</Field>

<div class="field-row">
  <div class="field-grow">
    <Field label={labels.cost} error={errors.cost} errorId="{idPrefix}-cost-err">
      <Input
        fill
        invalid={!!errors.cost}
        type="number"
        min={1}
        max={REWARD_COST_MAX}
        bind:value={draft.cost}
        aria-invalid={errors.cost ? 'true' : undefined}
        aria-describedby={errors.cost ? `${idPrefix}-cost-err` : undefined}
        required
      />
    </Field>
  </div>
  <div class="color-field">
    <Field label={labels.color}>
      <ColorPicker data-cursor={cursorColor ? '' : undefined} bind:value={draft.color} />
    </Field>
  </div>
</div>

<Field label={labels.cooldown} tag={labels.cooldownTag} error={errors.cooldown} errorId="{idPrefix}-cooldown-err">
  <DurationField
    bind:value={draft.cooldown}
    min={0}
    max={REWARD_COOLDOWN_MAX}
    label={labels.cooldown}
    invalid={!!errors.cooldown}
    describedby={errors.cooldown ? `${idPrefix}-cooldown-err` : undefined}
  />
</Field>

<Field label={labels.reply} tag={t('common.optional')}>
  <ResponseEditor bind:value={draft.replyMessage} {surface} placeholder={defaultReply} />
</Field>
<ChatPreview kind="reply" response={draft.replyMessage || defaultReply} showViewer={false} tag={previewTag} samples={replySamples} />

<Field label={labels.after}>
  <Select fill options={afterOptions} bind:value={draft.onRedeem} />
</Field>

<style>
  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-grow { flex: 1; min-width: 0; }

  .color-field { flex: none; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
