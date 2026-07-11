<script lang="ts">
  // Builder inspector for one trigger-word rule — the module twin of ReplyEditor,
  // extended with the two fields a trigger owns: the phrase to watch for and how
  // it matches. The response reuses the exact command builder surface
  // (ResponseEditor + its variable palette) and the ChatPreview rehearsal, so a
  // trigger reply is authored just like a command reply, tokens and all. Here the
  // rehearsal's viewer types a plain message containing the phrase (no "!"), which
  // is what actually fires the reply.
  //
  // Save/Cancel/Delete are handled by the page so the whole rule list persists in
  // one place.
  import { Icon, getI18n } from '@bagel/shared';
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
  const TOKENS = [
    { token: '{user}', label: '{user} → the chatter' },
    { token: '{random}', label: '{random} → a number 1-100' },
    { token: '{choice:a,b,c}', label: '{choice:a,b,c} → a random option' }
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
  <label class="field">
    <span>Trigger phrase</span>
    <input class="rule-input" type="text" placeholder="hello" bind:value={phrase} />
  </label>

  <label class="field">
    <span>Match</span>
    <select class="rule-input" bind:value={match}>
      {#each MODES as m (m.value)}<option value={m.value}>{m.label}</option>{/each}
    </select>
    <small>{modeHint}</small>
  </label>

  <div class="field">
    <span>Response</span>
    <ResponseEditor bind:value={message} placeholder={DEFAULT_RESPONSE} tokens={TOKENS} />
  </div>

  <ChatPreview
    name=""
    viewerText={sampleMessage}
    tag={`on "${phrase.trim() || 'hello'}"`}
    samples={{ user: 'sesame_sam', random: '42' }}
    response={effectiveMessage}
  />

  <div class="actions">
    {#if !isNew}
      <!-- Only an existing rule can be deleted; a new one is cancelled, not deleted. -->
      <button type="button" class="btn danger" onclick={onDelete} disabled={busy}>
        <Icon name="trash" size={14} /> Delete
      </button>
    {/if}
    <span class="spacer"></span>
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="button" class="btn primary" onclick={onSave} disabled={busy || !canSave}>
      <Icon name="check" size={14} />
      {busy ? t('modules.loading') : t('modules.saveChanges')}
    </button>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }
  .field { display: flex; flex-direction: column; gap: 8px; margin-bottom: 14px; }
  .field > span { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); letter-spacing: 0.01em; }
  .field small { color: var(--bb-muted); opacity: 0.75; font-size: 11px; }

  .rule-input {
    width: 100%;
    box-sizing: border-box;
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: 8px 8px;
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    transition: border-color var(--bb-dur-base, 160ms) ease, box-shadow var(--bb-dur-base, 160ms) ease;
  }
  .rule-input::placeholder { color: var(--bb-muted); opacity: 0.7; }
  .rule-input:focus {
    outline: none;
    border-color: rgba(82, 183, 136, 0.5);
    box-shadow: 0 0 0 3px rgba(82, 183, 136, 0.12);
    background: rgba(255, 255, 255, 0.04);
  }

  .actions { display: flex; align-items: center; gap: 10px; margin-top: 12px; }
  .spacer { flex: 1; }
  .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    border-radius: 8px 8px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    cursor: pointer;
    border: 1px solid var(--rule);
    background: transparent;
    color: var(--bb-muted);
    transition: all var(--bb-dur-fast, 140ms) ease;
  }
  .btn.ghost:hover { color: var(--bb-white); }
  .btn.primary { background: rgba(82, 183, 136, 0.16); border-color: rgba(82, 183, 136, 0.5); color: var(--bb-green-glow, #52b788); }
  .btn.primary:disabled { opacity: 0.5; cursor: default; }
  .btn.danger { border-color: transparent; color: #cf8a78; }
  .btn.danger:hover { border-color: rgba(176, 90, 70, 0.5); background: rgba(176, 90, 70, 0.08); }

  @media (max-width: 480px) {
    .actions { flex-wrap: wrap; }
    .actions .btn { flex: 1; justify-content: center; min-height: 44px; }
    .spacer { display: none; }
  }
</style>
