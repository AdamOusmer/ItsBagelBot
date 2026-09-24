<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Cluster, Field, getI18n } from '@bagel/kit';
  import { focusFirstInvalid } from '@bagel/kit';
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

  const DEFAULT_RESPONSE = $derived(t('modules.triggerDefaultResponse'));
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
    <input
      class="bb-input"
      type="text"
      placeholder={t('modules.triggerPhrasePh')}
      required
      data-invalid={phraseError ? '' : undefined}
      aria-invalid={phraseError ? 'true' : undefined}
      aria-describedby={phraseError ? PHRASE_ERR_ID : undefined}
      bind:value={phrase}
    />
  </Field>

  <Field label={t('modules.matchLabel')} hint={modeHint}>
    <select class="bb-input" bind:value={match}>
      {#each modes as m (m.value)}<option value={m.value}>{m.label}</option>{/each}
    </select>
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
    samples={{ user: 'sesame_sam' }}
    response={effectiveMessage}
  />

  <div class="actions">
  <Cluster gap={3}>
    {#if !isNew}
      <Button variant="destructive" onclick={onDelete} disabled={busy}>{t('common.delete')}</Button>
    {/if}
    <span class="spacer"></span>
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
    <Button onclick={save} disabled={busy}>
      {busy ? t('modules.loading') : t('modules.saveChanges')}
    </Button>
  </Cluster>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .actions { margin-top: 12px; }
  .spacer { flex: 1; }

  @media (max-width: 480px) {
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
    .spacer { display: none; }
  }
</style>
