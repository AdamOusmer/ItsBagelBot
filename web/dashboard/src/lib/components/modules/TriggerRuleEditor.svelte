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

  const MODES: { value: Match; label: string; hint: string }[] = [
    { value: 'word', label: 'Whole word', hint: 'Fires when the phrase appears as its own word (so "hi" will not fire inside "this").' },
    { value: 'contains', label: 'Contains', hint: 'Fires when the phrase appears anywhere, even inside another word.' },
    { value: 'exact', label: 'Exact message', hint: 'Fires only when the whole message equals the phrase.' },
    { value: 'prefix', label: 'Starts with', hint: 'Fires when the message begins with the phrase.' }
  ];

  // The response palette: the tokens sesame expands (module.ParseDynamic + {user}).
  // Labels go through t() like every other palette on this screen; they used to
  // be English literals, which is what made this the one reward-shaped surface
  // that stayed English under /fr.
  const TOKENS = [
    { token: '{user}', label: t('modules.trigTokUser') },
    { token: '{random}', label: t('modules.trigTokRandom') },
    { token: '{choice:a,b,c}', label: t('modules.trigTokChoice') }
  ];

  const DEFAULT_RESPONSE = 'hi {user}!';
  const effectiveMessage = $derived(message.trim() ? message : DEFAULT_RESPONSE);
  const modeHint = $derived(MODES.find((m) => m.value === match)?.hint ?? '');

  // A sample chat line that would fire this rule, shaped per match mode so the
  // rehearsed viewer message actually triggers the reply.
  const sampleMessage = $derived.by(() => {
    const p = phrase.trim() || 'hello';
    switch (match) {
      case 'exact':
        return p;
      case 'prefix':
        return `${p} everyone`;
      default:
        return `hey ${p} there`;
    }
  });

  const canSave = $derived(phrase.trim().length > 0 && message.trim().length > 0);
</script>

<div class="editor">
  <Field label="Trigger phrase">
    <input class="bb-input" type="text" placeholder="hello" bind:value={phrase} />
  </Field>

  <Field label="Match" hint={modeHint}>
    <select class="bb-input" bind:value={match}>
      {#each MODES as m (m.value)}<option value={m.value}>{m.label}</option>{/each}
    </select>
  </Field>

  <Field label="Response">
    <ResponseEditor bind:value={message} placeholder={DEFAULT_RESPONSE} tokens={TOKENS} />
  </Field>

  <!-- kind="reply": trigger replies expand only {user} plus the dynamic
       tokens (see app/twitch/sesame/modules/triggers.go firstReply). -->
  <ChatPreview
    kind="reply"
    name=""
    viewerText={sampleMessage}
    tag={`on "${phrase.trim() || 'hello'}"`}
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
      <Button variant="destructive" onclick={onDelete} disabled={busy}>Delete</Button>
    {/if}
    <span class="spacer"></span>
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
    <Button onclick={onSave} disabled={busy || !canSave}>
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
