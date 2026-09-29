<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select } from '@bagel/kit';
  import { namespaceReplyTemplate, namespaceReplySamples, moduleDef } from '@bagel/kit';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, Code, Field, EditorFooter, Input, SwitchRow, Text, getI18n, type GoveeDevice, type GoveeBinding } from '@bagel/kit';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import { focusFirstInvalid } from '@bagel/kit';

  let {
    device,
    binding,
    colors,
    busy = false,
    onSubmit,
    onCancel,
    onRequestDelete
  }: {
    device: GoveeDevice;
    binding: GoveeBinding | null;
    colors: string[];
    busy?: boolean;
    onSubmit: SubmitFunction;
    onCancel: () => void;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{govee:user} set the lights to {govee:color}!';
  const reply = moduleDef('govee')!.replies.find((reply) => reply.key === 'reply')!;
  const replySamples: Record<string, string> = namespaceReplySamples('govee', { user: 'sesame_sam', input: 'blue', color: 'Blue' });

  // svelte-ignore state_referenced_locally
  const reward = binding?.reward ?? null;
  // svelte-ignore state_referenced_locally
  const isNew = !binding?.rewardId;

  // svelte-ignore state_referenced_locally
  let title = $state(reward?.title || t('govee.defaultTitle', { name: device.name || t('govee.theLights') }));
  // svelte-ignore state_referenced_locally
  let cost = $state(reward?.cost ?? 500);
  // svelte-ignore state_referenced_locally
  let color = $state(reward?.color || '#9147ff');
  // svelte-ignore state_referenced_locally
  let cooldown = $state(reward?.cooldown ?? 0);
  // svelte-ignore state_referenced_locally
  let onRedeem = $state<string>(binding?.onRedeem ?? 'fulfill');
  // svelte-ignore state_referenced_locally
  let replyMessage = $state(namespaceReplyTemplate('govee', reply, binding?.replyMessage ?? ''));
  // svelte-ignore state_referenced_locally
  let allowOff = $state(binding?.allowOff ?? false);
  // svelte-ignore state_referenced_locally
  let liveOnly = $state(!binding?.allowOffline);

  const TITLE_ERR_ID = 'govee-title-err';
  let titleError = $state<string | undefined>(undefined);
  let formEl = $state<HTMLFormElement | null>(null);

  const submit: SubmitFunction = (input) => {
    titleError = title.trim() ? undefined : t('govee.errTitleRequired');
    if (titleError) {
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    replyMessage = namespaceReplyTemplate('govee', reply, replyMessage);
    input.formData.set('replyMessage', replyMessage);
    return onSubmit(input);
  };
</script>

<form method="POST" action="?/saveReward" class="editor" novalidate use:enhance={submit} bind:this={formEl}>
  <input type="hidden" name="device" value={device.device} />
  <input type="hidden" name="sku" value={device.sku} />
  <input type="hidden" name="deviceName" value={device.name} />

  <Text size="sm" tone="muted">
    {t('govee.editorHintNames')} <Code>{colors.join(', ')}</Code>. {t('govee.editorHintHex')} <Code>#00ccff</Code>.
  </Text>

  <Field label={t('govee.fieldTitle')} error={titleError} errorId={TITLE_ERR_ID}>
    <Input
      fill
      invalid={!!titleError}
      type="text"
      name="title"
      maxlength="45"
      bind:value={title}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? TITLE_ERR_ID : undefined}
      required
    />
  </Field>

  <div class="field-row">
    <div class="field-grow">
      <Field label={t('govee.fieldCost')}>
        <Input fill type="number" name="cost" min="1" max="10000000" bind:value={cost} required />
      </Field>
    </div>
    <div class="color-field">
      <Field label={t('govee.fieldColor')}>
        <span class="color-row">
          <span class="swatch"><Input type="color" name="color" bind:value={color} /></span>
          <Text as="span" size="xs" mono tone="accent">{color.toUpperCase()}</Text>
        </span>
      </Field>
    </div>
  </div>

  <Field label={t('govee.fieldCooldown')} tag={t('govee.fieldCooldownTag')}>
    <Input fill type="number" name="cooldown" min="0" max="604800" bind:value={cooldown} />
  </Field>

  <Field label={t('govee.fieldReply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={replyMessage} name="replyMessage" surface="reward:govee" placeholder={DEFAULT_REPLY} />
  </Field>
  <ChatPreview kind="reply" response={replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('govee.previewTag')} samples={replySamples} />

  <Field label={t('govee.afterTitle')}>
    <Select
      fill
      name="onRedeem" bind:value={onRedeem}
      options={[{ value: 'fulfill', label: t('govee.afterFulfill') }, { value: 'cancel', label: t('govee.afterCancel') }, { value: 'leave', label: t('govee.afterLeave') }]}
    />
  </Field>

  <div class="setrow" class:on={allowOff}>
    <SwitchRow
      control="end"
      bind:checked={allowOff}
      label={t('govee.allowOffLabel')}
      hint={t('govee.allowOffHint')}
      hintId="govee-allowoff-desc"
    />
  </div>
  <input type="hidden" name="allow_off" value={allowOff ? 'on' : ''} />

  <div class="setrow">
    <SwitchRow
      control="end"
      bind:checked={liveOnly}
      tone={liveOnly ? undefined : 'warn'}
      label={t('govee.liveOnlyLabel')}
      hint={liveOnly ? t('govee.liveOnlyOn') : t('govee.liveOnlyOff')}
      hintId="govee-liveonly-desc"
    />
  </div>
  <input type="hidden" name="allow_offline" value={liveOnly ? '' : 'on'} />

  {#if binding}
    <div class="del-row">
      <Button variant="destructive" onclick={onRequestDelete} disabled={busy}>{t('govee.deleteReward')}</Button>
    </div>
  {/if}

  <EditorFooter
    onCancel={onCancel}
    canSave={!busy}
    status={busy ? 'saving' : 'idle'}
    saveLabel={isNew ? t('govee.create') : t('govee.saveChanges')}
    savingLabel={t('govee.saving')}
    cancelLabel={t('common.cancel')}
  />
</form>

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
