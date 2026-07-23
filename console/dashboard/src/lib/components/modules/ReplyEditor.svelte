<script lang="ts">
  // Builder inspector for one module reply — the same surface as editing a custom
  // command's response: the shared ResponseEditor (message + token chips) and the
  // ChatPreview rehearsal (ItsBagelBot name + logo).
  //
  // Two reply shapes:
  //  - event replies (shoutout, alerts): framed by the firing event (`tag`),
  //    bot line only.
  //  - command replies (gateway modules: reply.command set): same surface as a
  //    custom command — "Chat rehearsal" border, a sample viewer typing the
  //    trigger — and the token palette swaps to the reply's supported variables.
  // Both rehearse with kind="reply": ONLY this reply's previewSamples (plus the
  //    dynamic tokens) substitute, so foreign tokens stay marked as unknown.
  //
  // Save/Cancel are handled by the page so the whole-module config persists in
  // one place.
  import { Icon, getI18n, type ModuleReply } from '@bagel/shared';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let {
    reply,
    message = $bindable(''),
    busy = false,
    onCancel,
    onSave
  }: {
    reply: ModuleReply;
    message: string;
    busy?: boolean;
    onCancel: () => void;
    onSave: () => void;
  } = $props();

  const { t } = getI18n();

  // Blank posts the module default, so preview the default (matches the
  // placeholder) instead of an empty "nothing to say yet".
  const effectiveMessage = $derived(message.trim() ? message : reply.defaultMessage);

  const isCommand = $derived(!!reply.command);
  // The reply's own insert palette; undefined keeps ResponseEditor's default
  // command tokens (event replies define no token list yet). The chip tooltip
  // shows the sample value the preview substitutes.
  const palette = $derived(
    reply.tokens?.map((tk) => {
      const token = `{${tk}}`;
      const sample = reply.previewSamples?.[tk];
      return { token, label: sample ? `${token} → ${sample}` : token };
    })
  );
</script>

<div class="editor">
  <label class="field">
    <span>{t('modules.replyMessage', { label: reply.label })}</span>
    <ResponseEditor bind:value={message} placeholder={reply.defaultMessage} tokens={palette} />
    <small>{t('modules.replyBlankHint')}</small>
  </label>

  {#if isCommand}
    <!-- Same surface as the commands page: viewer types the trigger, the bot
         answers. kind="reply" because sesame expands only this reply's own
         tokens (plus {random}/{choice:…}) — never the command set. -->
    <ChatPreview
      kind="reply"
      name={reply.command}
      args={reply.previewArgs ?? ''}
      samples={reply.previewSamples}
      response={effectiveMessage}
    />
  {:else}
    <ChatPreview
      kind="reply"
      name=""
      showViewer={false}
      tag={reply.event}
      samples={reply.previewSamples}
      response={effectiveMessage}
    />
  {/if}

  <div class="actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="button" class="btn primary" onclick={onSave} disabled={busy}>
      <Icon name="check" size={14} />
      {busy ? t('modules.loading') : t('modules.saveChanges')}
    </button>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }
  .field { display: flex; flex-direction: column; gap: 8px; margin-bottom: 14px; }
  .field > span { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); letter-spacing: 0.01em; }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 12px; }
  @media (max-width: 480px) {
    .actions { flex-direction: column-reverse; }
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
