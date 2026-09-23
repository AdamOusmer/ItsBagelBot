<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Builder inspector for one module reply, the same surface as editing a custom
  // command's response: the shared ResponseEditor (message + token chips) and the
  // ChatPreview rehearsal (ItsBagelBot name + logo).
  //
  // Two reply shapes:
  //  - event replies (shoutout, alerts): framed by the firing event (`tag`),
  //    bot line only.
  //  - command replies (gossip modules: reply.command set): same surface as a
  //    custom command ("Chat rehearsal" border, a sample viewer typing the
  //    trigger), and the token palette swaps to the reply's supported variables.
  // Both rehearse with kind="reply": ONLY this reply's own token samples
  //    (plus the dynamic tokens) substitute, so foreign tokens stay marked
  //    as unknown.
  //
  // Save/Cancel are handled by the page so the whole-module config persists in
  // one place.
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

  // Blank posts the module default, so preview the default (matches the
  // placeholder) instead of an empty "nothing to say yet".
  const localizedDefault = $derived(tModuleReplyDefault(t, moduleId, reply));
  const effectiveMessage = $derived(message.trim() ? message : localizedDefault);

  const isCommand = $derived(!!reply.command);
  // This reply's own token reference, read off the manifest surface (the same
  // set surface={{module,reply}} on ResponseEditor's VariablePalette
  // offers), for the read-only list under the rehearsal below.
  const ownTokens = $derived(chipsFor({ module: moduleId, reply: reply.key }));
  // ChatPreview's samples prop is a bare name->sample record (it feeds the
  // shared rehearsal in engine/rehearsal.ts, which knows nothing about
  // ReplyToken); derive it from the same tokens the palette reads so the two
  // never disagree.
  const rehearsalSamples = $derived(Object.fromEntries((reply.tokens ?? []).map((tk) => [tk.name, tk.sample])));
</script>

<div class="editor">
  <Field label={t('modules.replyMessage', { label: tModuleReplyPart(t, moduleId, reply, 'label') })} hint={t('modules.replyBlankHint')}>
    <ResponseEditor bind:value={message} placeholder={localizedDefault} surface={{ module: moduleId, reply: reply.key }} />
  </Field>

  {#if isCommand}
    <!-- Same surface as the commands page: viewer types the trigger, the bot
         answers. kind="reply" because sesame expands only this reply's own
         tokens (plus {random}/{choice:…}): never the command set. -->
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
