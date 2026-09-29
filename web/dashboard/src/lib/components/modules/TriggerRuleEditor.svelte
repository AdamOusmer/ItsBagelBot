<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Cluster, Field, Input, Select, focusFirstInvalid } from '@bagel/ui/svelte';
  import { getI18n, namespaceReplyTemplate } from '@bagel/kit';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  export type Match = 'word' | 'contains' | 'exact' | 'prefix';

  let {
    phrase = $bindable(''),
    match = $bindable('word' as Match),
    message = $bindable(''),
    busy = false,
    isNew = false,
    onSave,
    onCancel,
    onDelete
  }: {
    phrase: string;
    match: Match;
    message: string;
    busy?: boolean;
    isNew?: boolean;
    onSave: () => void;
    onCancel: () => void;
    onDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const modes = $derived([
    { value: 'word' as Match, label: t('modules.matchWord'), hint: t('modules.matchHintWord') },
    { value: 'contains' as Match, label: t('modules.matchContains'), hint: t('modules.matchHintContains') },
    { value: 'exact' as Match, label: t('modules.matchExact'), hint: t('modules.matchHintExact') },
    { value: 'prefix' as Match, label: t('modules.matchPrefix'), hint: t('modules.matchHintPrefix') }
  ]);

  const DEFAULT_RESPONSE = $derived(namespaceReplyTemplate('triggers', { tokens: [{ name: 'triggers:user', sample: '' }, { name: 'triggers:channel', sample: '' }] }, t('modules.triggerDefaultResponse')));
  const effectiveMessage = $derived(message.trim() ? message : DEFAULT_RESPONSE);
  const modeHint = $derived(modes.find((m) => m.value === match)?.hint ?? '');

  const sampleMessage = $derived.by(() => {
    const p = phrase.trim() || t('modules.triggerPhraseExample');
    switch (match) {
      case 'exact':
        return p;
      case 'prefix':
        return `${p} everyone`;
      default:
        return `hey ${p} there`;
    }
  });

  const PHRASE_ERR_ID = 'trigger-phrase-err';
  const RESPONSE_ERR_ID = 'trigger-response-err';
  let attempted = $state(false);
  let editorEl = $state<HTMLDivElement | null>(null);
  const phraseError = $derived(attempted && !phrase.trim() ? t('modules.errTriggerPhraseRequired') : undefined);
  const responseError = $derived(attempted && !message.trim() ? t('modules.errTriggerResponseRequired') : undefined);

  function save() {
    attempted = true;
    if (!phrase.trim() || !message.trim()) {
      void focusFirstInvalid(editorEl);
      return;
    }
    onSave();
  }
</script>

<div class="editor" bind:this={editorEl}>
  <Field label={t('modules.triggerPhrase')} error={phraseError} errorId={PHRASE_ERR_ID}>
    <Input
      placeholder={t('modules.triggerPhrasePh')}
      required
      invalid={!!phraseError}
      aria-invalid={phraseError ? 'true' : undefined}
      aria-describedby={phraseError ? PHRASE_ERR_ID : undefined}
      bind:value={phrase}
    />
  </Field>

  <Field label={t('modules.matchLabel')} hint={modeHint}>
    <Select
      fill
      bind:value={match}
      options={modes}
    />
  </Field>

  <Field label={t('modules.responseLabel')} error={responseError} errorId={RESPONSE_ERR_ID}>
    <ResponseEditor
      bind:value={message}
      placeholder={DEFAULT_RESPONSE}
      surface="triggers"
      required
      invalid={!!responseError}
      describedby={responseError ? RESPONSE_ERR_ID : undefined}
    />
  </Field>

  <ChatPreview
    kind="reply"
    name=""
    viewerText={sampleMessage}
    tag={t('modules.triggerPreviewTag', { phrase: phrase.trim() || t('modules.triggerPhraseExample') })}
    samples={{ 'triggers:user': 'sesame_sam', 'triggers:channel': 'bagel_stream' }}
    response={effectiveMessage}
  />

  <div class="actions">
    {#if !isNew}
      <Button onclick={onDelete} disabled={busy} tone="danger">{t('common.delete')}</Button>
    {/if}
    <div class="end">
      <Cluster gap={3}>
        <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
        <Button onclick={save} disabled={busy}>
          {busy ? t('modules.loading') : t('modules.saveChanges')}
        </Button>
      </Cluster>
    </div>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--bb-space-3);
    margin-top: 12px;
  }
  .end { margin-left: auto; }

  @media (max-width: 1079px) {
    .actions {
      position: sticky;
      bottom: 0;
      padding: 12px 0;
      background: var(--bb-bg-1);
      border-top: 1px solid var(--bb-border);
    }
  }
  @media (max-width: 480px) {
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
    .end { width: 100%; margin-left: 0; }
  }
</style>
