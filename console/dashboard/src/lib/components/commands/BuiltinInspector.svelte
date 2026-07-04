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
    getI18n,
    PERM_LABELS,
    type CommandView,
    type BuiltinCommandDef,
    type Perm
  } from '@bagel/shared';

  let {
    command,
    def,
    toggleSubmit
  }: {
    command: CommandView;
    def: BuiltinCommandDef;
    toggleSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const c = $derived(command);
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
      <button
        class="toggle {c.is_active ? 'on' : ''}"
        type="submit"
        aria-label={t('commandRow.toggleAria', { name: c.name })}
      ></button>
    </form>
  </div>

  <p class="desc">{def.description}</p>

  <div class="field">
    <span>{t('builtinInspector.usage')}</span>
    <ul class="usage">
      {#each def.usage as u}<li><code>{u}</code></li>{/each}
    </ul>
  </div>

  <div class="field">
    <span>{t('builtinInspector.preview')}</span>
    <div class="chat" aria-label={t('chatPreview.ariaPreview')}>
      <span class="chat-tag">{t('chatPreview.rehearsal')}</span>
      <div class="line bot">
        <span class="bot-name">bot</span>
        <span class="msg">{def.preview}</span>
      </div>
    </div>
  </div>

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
    border-radius: 6px;
    padding: 4px 8px;
  }

  /* Read-only value box: styled like a disabled .search input so Access /
     Cooldown read as fields, just non-editable. */
  .ro {
    box-sizing: border-box;
    width: 100%;
    padding: 9px 12px;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(255, 255, 255, 0.02);
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 13px;
  }

  /* Preview card mirrors ChatPreview's rehearsal look (green left rule, floating
     tag, bot line) so the preview matches the custom-command editor's rehearsal. */
  .chat {
    position: relative;
    padding: 14px 14px 12px;
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-left: 2px solid rgba(82, 183, 136, 0.5);
    border-radius: var(--bb-radius-sm, 6px);
    background: rgba(0, 0, 0, 0.3);
  }
  .chat-tag {
    position: absolute;
    top: -8px;
    left: 10px;
    background: var(--bb-bg-0, #0a0a0a);
    padding: 0 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 9.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .line.bot {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  .bot-name {
    flex: none;
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 12.5px;
    color: var(--bb-green-glow);
  }
  .bot-name::after {
    content: ':';
    color: var(--bb-muted);
    font-weight: 400;
  }
  .msg {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    line-height: 1.5;
    word-break: break-word;
  }
</style>
