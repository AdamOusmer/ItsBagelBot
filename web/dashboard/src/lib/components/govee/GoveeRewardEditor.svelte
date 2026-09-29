<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select, namespaceReplySamples, Button, Code, Field, Input, SwitchRow, Text, getI18n } from '@bagel/kit';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import DurationField from '$lib/components/shared/DurationField.svelte';
  import { GOVEE_COOLDOWN_MAX, goveeErrors, type GoveeDraft, type GoveeErrorField } from './govee-draft';

  let {
    draft = $bindable<GoveeDraft>(),
    colors,
    canDelete,
    busy = false,
    attempted = false,
    onRequestDelete
  }: {
    draft: GoveeDraft;
    colors: string[];
    canDelete: boolean;
    busy?: boolean;
    attempted?: boolean;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{govee:user} set the lights to {govee:color}!';
  const replySamples: Record<string, string> = namespaceReplySamples('govee', { user: 'sesame_sam', input: 'blue', color: 'Blue' });

  const errors = $derived(goveeErrors(draft));
  const shown = (field: GoveeErrorField) => (attempted && errors[field] ? t(errors[field]!) : undefined);
  const titleError = $derived(shown('title'));
  const costError = $derived(shown('cost'));
  const cooldownError = $derived(shown('cooldown'));
</script>

<div class="editor">
  <Text size="sm" tone="muted">
    {t('govee.editorHintNames')} <Code>{colors.join(', ')}</Code>. {t('govee.editorHintHex')} <Code>#00ccff</Code>.
  </Text>

  <Field label={t('govee.fieldTitle')} error={titleError} errorId="govee-title-err">
    <Input
      fill
      invalid={!!titleError}
      type="text"
      maxlength="45"
      bind:value={draft.title}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? 'govee-title-err' : undefined}
      required
    />
  </Field>

  <div class="field-row">
    <div class="field-grow">
      <Field label={t('govee.fieldCost')} error={costError} errorId="govee-cost-err">
        <Input
          fill
          invalid={!!costError}
          type="number"
          min="1"
          max="10000000"
          bind:value={draft.cost}
          aria-invalid={costError ? 'true' : undefined}
          aria-describedby={costError ? 'govee-cost-err' : undefined}
          required
        />
      </Field>
    </div>
    <div class="color-field">
      <Field label={t('govee.fieldColor')}>
        <span class="color-row">
          <span class="swatch"><Input type="color" bind:value={draft.color} /></span>
          <Text as="span" size="xs" mono tone="accent">{draft.color.toUpperCase()}</Text>
        </span>
      </Field>
    </div>
  </div>

  <Field label={t('govee.fieldCooldown')} tag={t('govee.fieldCooldownTag')} error={cooldownError} errorId="govee-cooldown-err">
    <DurationField
      bind:value={draft.cooldown}
      min={0}
      max={GOVEE_COOLDOWN_MAX}
      label={t('govee.fieldCooldown')}
      invalid={!!cooldownError}
      describedby={cooldownError ? 'govee-cooldown-err' : undefined}
    />
  </Field>

  <Field label={t('govee.fieldReply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={draft.replyMessage} surface="reward:govee" placeholder={DEFAULT_REPLY} />
  </Field>
  <ChatPreview kind="reply" response={draft.replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('govee.previewTag')} samples={replySamples} />

  <Field label={t('govee.afterTitle')}>
    <Select
      fill
      bind:value={draft.onRedeem}
      options={[{ value: 'fulfill', label: t('govee.afterFulfill') }, { value: 'cancel', label: t('govee.afterCancel') }, { value: 'leave', label: t('govee.afterLeave') }]}
    />
  </Field>

  <div class="setrow" class:on={draft.allowOff}>
    <SwitchRow
      control="end"
      bind:checked={draft.allowOff}
      label={t('govee.allowOffLabel')}
      hint={t('govee.allowOffHint')}
      hintId="govee-allowoff-desc"
    />
  </div>

  <div class="setrow">
    <SwitchRow
      control="end"
      bind:checked={draft.liveOnly}
      tone={draft.liveOnly ? undefined : 'warn'}
      label={t('govee.liveOnlyLabel')}
      hint={draft.liveOnly ? t('govee.liveOnlyOn') : t('govee.liveOnlyOff')}
      hintId="govee-liveonly-desc"
    />
  </div>

  {#if canDelete}
    <div class="del-row">
      <Button variant="destructive" type="button" onclick={onRequestDelete} disabled={busy}>{t('govee.deleteReward')}</Button>
    </div>
  {/if}
</div>

<style>
  .editor { --field-mb: 0; padding: 4px 2px 2px; display: grid; gap: 14px; }

  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-grow { flex: 1; min-width: 0; }

  .color-field { flex: none; width: 116px; }
  .color-row { display: flex; align-items: center; gap: 8px; }
  .swatch { display: block; flex: none; width: 44px; }

  .setrow {
    padding: 11px 12px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
  }
  .setrow.on { border-color: rgba(var(--bb-tan-rgb), 0.45); background: rgba(var(--bb-tan-rgb), 0.06); }

  .del-row { display: flex; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
