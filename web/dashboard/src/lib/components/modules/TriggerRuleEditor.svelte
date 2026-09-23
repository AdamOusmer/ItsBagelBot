<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Builder inspector for one trigger-word rule, the module twin of ReplyEditor,
  // extended with the two fields a trigger owns: the phrase to watch for and how
  // it matches. The response reuses the exact command builder surface
  // (ResponseEditor + its variable palette) and the ChatPreview rehearsal, so a
  // trigger reply is authored just like a command reply, tokens and all. Here the
  // rehearsal's viewer types a plain message containing the phrase (no "!"), which
  // is what actually fires the reply.
  //
  // Save/Cancel/Delete are handled by the page so the whole rule list persists in
  // one place.
  import { Button, Cluster, Field, getI18n } from '@bagel/kit';
  import { focusFirstInvalid } from '@bagel/kit';
  import { chipsFor } from '@bagel/kit/variables';
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

  // The response palette: the tokens sesame expands (module.ParseDynamic +
  // {user}), read off the manifest's own user/random/choice Variables
  // (surfaces.ts's forSurface('triggers')) instead of a hand-kept literal
  // list — those three chips now carry the exact same worked examples and
  // hint copy (vars.<id>.hint) as the custom-command palette.
  const TOKENS = chipsFor('triggers').map((c) => ({ token: c.token, hint: c.hintKey }));

  const DEFAULT_RESPONSE = $derived(t('modules.triggerDefaultResponse'));
  const effectiveMessage = $derived(message.trim() ? message : DEFAULT_RESPONSE);
  const modeHint = $derived(modes.find((m) => m.value === match)?.hint ?? '');

  // A sample chat line that would fire this rule, shaped per match mode so the
  // rehearsed viewer message actually triggers the reply.
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

  // Issue #967 exposed the same silent gate as the reward editor: the example
  // text looked like a value while Save was disabled. Let Save explain what's
  // missing, associate the feedback with each control, and focus the first one.
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
      tokens={TOKENS}
      required
      invalid={!!responseError}
      describedby={responseError ? RESPONSE_ERR_ID : undefined}
    />
  </Field>

  <!-- kind="reply": trigger replies expand only {user} plus the dynamic
       tokens (see app/twitch/sesame/modules/triggers.go firstReply). -->
  <ChatPreview
    kind="reply"
    name=""
    viewerText={sampleMessage}
    tag={t('modules.triggerPreviewTag', { phrase: phrase.trim() || t('modules.triggerPhraseExample') })}
    samples={{ user: 'sesame_sam' }}
    response={effectiveMessage}
  />

  <!-- Blocks, not a local re-skin. `.rule-btn` was this editor's own drawing
       of a button (body font, 13px, its own quiet red for Delete) laid ON TOP
       of `.bb-btn`, so three of its rules reached into the shared contract to
       undo parts of it. Delete is the destructive variant now and the row is
       a Cluster: the look changes slightly, the number of button definitions
       in this repo goes from two to one. -->
  <div class="actions">
  <Cluster gap={3}>
    {#if !isNew}
      <!-- Only an existing rule can be deleted; a new one is cancelled, not deleted. -->
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

  /* Composition only. The fields are `Field` blocks, the controls wear
     `.bb-input` and the buttons are `Button` blocks; what is left here is
     where this editor's action row sits and how it behaves on a phone. */
  .actions { margin-top: 12px; }
  .spacer { flex: 1; }

  @media (max-width: 480px) {
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
    .spacer { display: none; }
  }
</style>
