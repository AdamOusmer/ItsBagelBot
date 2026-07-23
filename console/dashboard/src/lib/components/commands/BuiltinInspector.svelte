<script lang="ts">
  // Inspector pane for a built-in command. Built-ins have no editable response,
  // so this is intentionally read-only: it shows what the command does, example
  // usage, a preview of the chat line the bot posts, and a single on/off toggle.
  // The toggle posts to ?/toggleBuiltin (built-in state lives in the modules
  // service) via the page's shared optimistic toggle handler.
  //
  // The layout mirrors CommandEditor — same field labels, spacing, and a
  // rehearsal-style preview card — so switching between a custom command and a
  // built-in in the same docked inspector reads as one consistent surface.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Switch,
    getI18n,
    PERM_LABELS,
    type CommandView,
    type BuiltinCommandDef,
    type Perm
  } from '@bagel/shared';
  import ChatPreview from './ChatPreview.svelte';
  import ResponseEditor from './ResponseEditor.svelte';

  let {
    command,
    def,
    toggleSubmit,
    replySubmit,
    busy = false
  }: {
    command: CommandView;
    def: BuiltinCommandDef;
    toggleSubmit: SubmitFunction;
    // replySubmit persists an editable built-in's reply template (only used when
    // def.editable). Omitted for read-only built-ins.
    replySubmit?: SubmitFunction;
    busy?: boolean;
  } = $props();

  const { t } = getI18n();
  const c = $derived(command);

  // --- Editable reply (e.g. clip) ------------------------------------------
  // Seed the editor from the saved template (command.response), and re-seed when
  // switching to a different built-in row so the draft never leaks across rows.
  // seededFor starts null so the first effect run seeds it; it re-seeds only on
  // an actual row change, so editing (which leaves the name unchanged) never
  // fights the seed.
  let message = $state('');
  let seededFor = $state<string | null>(null);
  $effect(() => {
    if (command.name !== seededFor) {
      seededFor = command.name;
      message = command.response;
    }
  });

  // The insert palette: the built-in's own tokens, each chip tooltip showing the
  // sample value the rehearsal substitutes (mirrors the module ReplyEditor).
  const palette = $derived(
    (def.tokens ?? []).map((tk) => {
      const token = `{${tk}}`;
      const sample = def.previewSamples?.[tk];
      return { token, label: sample ? `${token} → ${sample}` : token };
    })
  );
  // Blank posts the default template, so preview the default instead of "nothing
  // to say yet".
  const effectiveMessage = $derived(message.trim() ? message : def.preview);
</script>

<div class="editor builtin">
  <!-- Enabled: horizontal toggle row with a hairline under it -->
  <div class="toggle-row">
    <div class="tr-text">
      <span class="tr-label">{t('builtinInspector.enabled')}</span>
      <span class="tr-help">{t('builtinInspector.enabledHelp')}</span>
    </div>
    <form method="POST" action="?/toggleBuiltin" use:enhance={toggleSubmit}>
      <input type="hidden" name="name" value={c.name} />
      <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
      <Switch type="submit" checked={c.is_active} label={t('commandRow.toggleAria', { name: c.name })} />
    </form>
  </div>

  <p class="desc">{def.description}</p>

  <div class="field">
    <span>{t('builtinInspector.usage')}</span>
    <ul class="usage">
      {#each def.usage as u}<li><code>{u}</code></li>{/each}
    </ul>
  </div>

  {#if def.editable && replySubmit}
    <!-- Editable reply: same surface as a custom command — message editor with a
         token palette, then the chat rehearsal. Saved to the modules-service
         config (via ?/saveBuiltinReply), not the commands service. -->
    <form class="reply-form" method="POST" action="?/saveBuiltinReply" use:enhance={replySubmit}>
      <input type="hidden" name="name" value={c.name} />
      <input type="hidden" name="is_active" value={c.is_active ? 'on' : ''} />
      <div class="field">
        <span>{t('builtinInspector.replyMessage')}</span>
        <ResponseEditor name="reply" bind:value={message} tokens={palette} placeholder={def.preview} />
        <small class="hint">{t('builtinInspector.replyHint')}</small>
      </div>
      <!-- kind="reply": built-in replies are expanded by a bare token replacer
           (e.g. clipExpand) — only def.previewSamples substitute, no dynamic
           tokens, no slash-verb routing. -->
      <ChatPreview
        kind="reply"
        dynamic={false}
        name={def.id}
        args={def.previewArgs ?? ''}
        response={effectiveMessage}
        samples={def.previewSamples}
      />
      <div class="reply-actions">
        <button class="btn primary" type="submit" disabled={busy}>
          <Icon name="check" size={14} />
          {t('builtinInspector.saveReply')}
        </button>
      </div>
    </form>
  {:else}
    <div class="field">
      <span>{t('builtinInspector.preview')}</span>
      <ChatPreview kind="reply" dynamic={false} name={def.id} args={def.previewArgs ?? ''} response={def.preview} samples={def.previewSamples} />
    </div>
  {/if}

  <div class="field-row">
    <div class="field">
      <span>{t('builtinInspector.access')}</span>
      <div class="ro">{PERM_LABELS[(c.perm ?? def.defaultPerm) as Perm]}</div>
    </div>
    <div class="field">
      <span>{t('builtinInspector.cooldown')}</span>
      <div class="ro">{c.cooldown ?? def.defaultCooldown}s</div>
    </div>
  </div>
</div>

<style>
  /* Match CommandEditor's container + field rhythm exactly. */
  .editor {
    padding: 4px 2px 2px;
    font-family: var(--bb-font-body);
  }

  /* Enabled toggle sits in a row with a hairline beneath, like the top of the
     module form; the switch is the shared global .toggle. */
  .toggle-row {
    display: flex;
    align-items: center;
    gap: 14px;
    padding-bottom: 16px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--rule, var(--glass-border));
  }
  .tr-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .tr-label {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .tr-help {
    font-size: 12px;
    color: var(--bb-muted);
    line-height: 1.4;
  }
  .toggle-row form {
    margin-left: auto;
  }

  .desc {
    margin: 0 0 16px;
    font-size: 13px;
    line-height: 1.6;
    color: var(--bb-muted);
  }

  /* Field label typography, copied from CommandEditor. */
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;
  }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }

  .reply-form { margin-bottom: 14px; }
  .field .hint { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .reply-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
  @media (max-width: 480px) {
    .reply-actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }

  .field-row {
    display: flex;
    gap: 12px;
  }
  .field-row .field {
    flex: 1;
    min-width: 0;
  }

  .usage {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .usage code {
    font-family: var(--bb-font-mono);
    font-size: 12.5px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    padding: 4px 8px;
  }

  /* Read-only value box: styled like a disabled .search input so Access /
     Cooldown read as fields, just non-editable. */
  .ro {
    box-sizing: border-box;
    width: 100%;
    padding: 9px 12px;
    border: 1px solid var(--glass-border);
    border-radius: 8px 8px;
    background: rgba(255, 255, 255, 0.02);
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 13px;
  }
</style>
