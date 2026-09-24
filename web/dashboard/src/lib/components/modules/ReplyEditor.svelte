<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Field, getI18n, tModuleReplyDefault, tModuleReplyPart, type ModuleReply } from '@bagel/kit';
  import { chipsFor } from '@bagel/kit/variables';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import ReplyTokenList from '$lib/components/variables/ReplyTokenList.svelte';

  let {
    moduleId,
    reply,
    message = $bindable(''),
    busy = false,
    onCancel,
    onSave
  }: {
    moduleId: string;
    reply: ModuleReply;
    message: string;
    busy?: boolean;
    onCancel: () => void;
    onSave: () => void;
  } = $props();

  const { t } = getI18n();

  const localizedDefault = $derived(tModuleReplyDefault(t, moduleId, reply));
  const effectiveMessage = $derived(message.trim() ? message : localizedDefault);

  const isCommand = $derived(!!reply.command);
  const ownTokens = $derived(chipsFor({ module: moduleId, reply: reply.key }));
  const rehearsalSamples = $derived(Object.fromEntries((reply.tokens ?? []).map((tk) => [tk.name, tk.sample])));
</script>

<div class="editor">
  <Field label={t('modules.replyMessage', { label: tModuleReplyPart(t, moduleId, reply, 'label') })} hint={t('modules.replyBlankHint')}>
    <ResponseEditor bind:value={message} placeholder={localizedDefault} surface={{ module: moduleId, reply: reply.key }} />
  </Field>

  {#if isCommand}
    <ChatPreview
      kind="reply"
      name={reply.command}
      args={reply.previewArgs ?? ''}
      samples={rehearsalSamples}
      response={effectiveMessage}
    />
  {:else}
    <ChatPreview
      kind="reply"
      name=""
      showViewer={false}
      tag={tModuleReplyPart(t, moduleId, reply, 'event')}
      samples={rehearsalSamples}
      response={effectiveMessage}
    />
  {/if}

  <ReplyTokenList chips={ownTokens} />

  <div class="actions">
    <button type="button" class="bb-btn bb-btn--ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="button" class="bb-btn bb-btn--primary" onclick={onSave} disabled={busy}>
      {busy ? t('modules.loading') : t('modules.saveChanges')}
    </button>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }
  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 12px; }
  @media (max-width: 480px) {
    .actions { flex-direction: column-reverse; }
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
  }
</style>
