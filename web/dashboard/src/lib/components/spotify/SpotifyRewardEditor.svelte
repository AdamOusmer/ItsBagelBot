<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { namespaceReplyTemplate, namespaceReplySamples, moduleDef } from '@bagel/kit';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, Code, Field, EditorFooter, Text, getI18n, type SpotifyRedeemConfig } from '@bagel/kit';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import { focusFirstInvalid } from '@bagel/kit';

  let {
    redeem,
    busy = false,
    onSubmit,
    onCancel,
    onRequestDelete
  }: {
    redeem: SpotifyRedeemConfig;
    busy?: boolean;
    onSubmit: SubmitFunction;
    onCancel: () => void;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{songqueue:user} queued {songqueue:track}!';
  const reply = moduleDef('songqueue')!.replies.find((reply) => reply.key === 'redeem')!;
  const replySamples: Record<string, string> = namespaceReplySamples('songqueue', {
    user: t('spotify.previewUserSample'),
    track: 'Never Gonna Give You Up',
    input: 'rick roll',
    pos: '3'
  });

  // svelte-ignore state_referenced_locally
  const isNew = !redeem.rewardId;

  // svelte-ignore state_referenced_locally
  let title = $state(redeem.reward?.title || t('spotify.defaultTitle'));
  // svelte-ignore state_referenced_locally
  let cost = $state(redeem.reward?.cost ?? 500);
  // svelte-ignore state_referenced_locally
  let color = $state(redeem.reward?.color || '#1db954');
  // svelte-ignore state_referenced_locally
  let cooldown = $state(redeem.reward?.cooldown ?? 0);
  // svelte-ignore state_referenced_locally
  let onRedeem = $state<string>(redeem.onRedeem ?? 'fulfill');
  // svelte-ignore state_referenced_locally
  let replyMessage = $state(namespaceReplyTemplate('songqueue', reply, redeem.replyMessage ?? ''));

  const TITLE_ERR_ID = 'spotify-title-err';
  let titleError = $state<string | undefined>(undefined);
  let formEl = $state<HTMLFormElement | null>(null);

  const submit: SubmitFunction = (input) => {
    titleError = title.trim() ? undefined : t('spotify.errTitleRequired');
    if (titleError) {
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    replyMessage = namespaceReplyTemplate('songqueue', reply, replyMessage);
    input.formData.set('replyMessage', replyMessage);
    return onSubmit(input);
  };
</script>

<form method="POST" action="?/saveReward" class="editor" novalidate use:enhance={submit} bind:this={formEl}>
  <Text size="sm" tone="muted">
    {t('spotify.editorInputHint')} <Code>Blinding Lights</Code>. {t('spotify.editorInputHintPair')}
    <Code>The Weeknd - Blinding Lights</Code>. {t('spotify.editorInputHintLink')}
  </Text>

  <Field label={t('spotify.fieldTitle')} error={titleError} errorId={TITLE_ERR_ID}>
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
      <Field label={t('spotify.fieldCost')}>
        <Input fill type="number" name="cost" min="1" max="10000000" bind:value={cost} required />
      </Field>
    </div>
    <div class="color-field">
      <Field label={t('spotify.fieldColor')}>
        <span class="color-row">
          <span class="swatch"><Input data-cursor type="color" name="color" bind:value={color} /></span>
          <Text as="span" size="xs" mono tone="accent">{color.toUpperCase()}</Text>
        </span>
      </Field>
    </div>
  </div>

  <Field label={t('spotify.fieldCooldown')} tag={t('spotify.fieldCooldownTag')}>
    <Input fill type="number" name="cooldown" min="0" max="604800" bind:value={cooldown} />
  </Field>

  <Field label={t('spotify.fieldReply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={replyMessage} name="replyMessage" surface="reward:spotify" placeholder={DEFAULT_REPLY} />
  </Field>
  <ChatPreview kind="reply" response={replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('spotify.previewTag')} samples={replySamples} />

  <Field label={t('spotify.afterTitle')}>
    <Select
      fill
      name="onRedeem"
      options={[
        { value: 'fulfill', label: t('spotify.afterFulfill') },
        { value: 'cancel', label: t('spotify.afterCancel') },
        { value: 'leave', label: t('spotify.afterLeave') }
      ]}
      bind:value={onRedeem}
    />
  </Field>

  {#if !isNew}
    <div class="del-row">
      <Button variant="destructive" onclick={onRequestDelete} disabled={busy}>{t('spotify.deleteReward')}</Button>
    </div>
  {/if}

  <EditorFooter
    onCancel={onCancel}
    canSave={!busy}
    status={busy ? 'saving' : 'idle'}
    saveLabel={isNew ? t('spotify.create') : t('spotify.saveChanges')}
    savingLabel={t('spotify.saving')}
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

  .del-row { display: flex; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
