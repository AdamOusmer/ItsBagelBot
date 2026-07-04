<script lang="ts">
  // Builder inspector for one module reply — the same surface as editing a custom
  // command's response: the shared ResponseEditor (message + token chips) and the
  // ChatPreview rehearsal (ItsBagelBot name + logo), framed by the firing event
  // instead of a viewer typing a command. Save/Cancel are handled by the page so
  // the whole-module config persists in one place.
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
</script>

<div class="editor">
  <label class="field">
    <span>{t('modules.replyMessage', { label: reply.label })}</span>
    <ResponseEditor bind:value={message} placeholder={reply.defaultMessage} />
    <small>{t('modules.replyBlankHint')}</small>
  </label>

  <ChatPreview name="" showViewer={false} tag={reply.event} response={effectiveMessage} />

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
