<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select, namespaceReplySamples, Button, Code, Field, Switch, getI18n, type GoveeDevice } from '@bagel/kit';
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
  <p class="hint">
    {t('govee.editorHintNames')} <Code>{colors.join(', ')}</Code>. {t('govee.editorHintHex')} <Code>#00ccff</Code>.
  </p>

  <Field label={t('govee.fieldTitle')} error={titleError} errorId="govee-title-err">
    <input
      class="bb-input"
      type="text"
      maxlength="45"
      bind:value={draft.title}
      data-invalid={titleError ? '' : undefined}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? 'govee-title-err' : undefined}
      required
    />
  </Field>

  <div class="field-row">
    <div class="field-grow">
    <Field label={t('govee.fieldCost')} error={costError} errorId="govee-cost-err">
      <input
        class="bb-input"
        type="number"
        min="1"
        max="10000000"
        bind:value={draft.cost}
        data-invalid={costError ? '' : undefined}
        aria-invalid={costError ? 'true' : undefined}
        aria-describedby={costError ? 'govee-cost-err' : undefined}
        required
      />
    </Field>
    </div>
    <label class="color-field">
      <span class="color-label">{t('govee.fieldColor')}</span>
      <span class="color-row">
        <input class="color-in" type="color" bind:value={draft.color} />
        <span class="color-hex">{draft.color}</span>
      </span>
    </label>
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

  <div class="setrow {draft.allowOff ? 'on' : ''}">
    <div class="setrow-text">
      <span class="setrow-label">{t('govee.allowOffLabel')}</span>
      <span class="muted-text" id="govee-allowoff-desc">{t('govee.allowOffHint')}</span>
    </div>
    <Switch bind:checked={draft.allowOff} label={t('govee.allowOffLabel')} describedby="govee-allowoff-desc" />
  </div>

  <div class="setrow {draft.liveOnly ? '' : 'warn'}">
    <div class="setrow-text">
      <span class="setrow-label">{t('govee.liveOnlyLabel')}</span>
      <span class="muted-text" id="govee-liveonly-desc">{draft.liveOnly ? t('govee.liveOnlyOn') : t('govee.liveOnlyOff')}</span>
    </div>
    <Switch bind:checked={draft.liveOnly} label={t('govee.liveOnlyLabel')} describedby="govee-liveonly-desc" />
  </div>

  {#if canDelete}
    <div class="del-row">
      <Button variant="destructive" type="button" onclick={onRequestDelete} disabled={busy}>{t('govee.deleteReward')}</Button>
    </div>
  {/if}
</div>

<style>
  .editor { padding: 4px 2px 2px; display: grid; gap: 14px; }
  .hint { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }


  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-grow { flex: 1; min-width: 0; }

  .color-field { display: flex; flex-direction: column; gap: 6px; flex: none; width: 116px; }
  .color-label { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .color-row { display: flex; align-items: center; gap: 8px; }
  .color-in {
    width: 44px;
    height: 37px;
    padding: 3px;
    border: 1px solid var(--rule);
    border-radius: var(--bb-radius-sm);
    background: rgba(240, 236, 228, 0.04);
    cursor: pointer;
    flex: none;
  }
  .color-hex { font-family: var(--bb-font-mono, monospace); font-size: 12px; color: var(--bb-tan-light); text-transform: uppercase; }

  .setrow {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 12px;
    border: 1px solid var(--rule);
    border-radius: var(--bb-radius-sm);
  }
  .setrow.on { border-color: var(--rule-tan); background: rgba(201, 168, 124, 0.06); }
  .setrow-text { display: grid; gap: 2px; flex: 1; min-width: 0; }
  .setrow-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }
  .setrow.warn .setrow-label { color: #d9a441; }
  .muted-text { margin: 0; font-family: var(--bb-font-body); font-size: 12px; line-height: 1.5; color: var(--bb-muted); }

  .del-row { display: flex; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
