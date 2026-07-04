<script lang="ts">
  // Inspector pane for a built-in command. Built-ins have no editable response,
  // so this is intentionally read-only: it shows what the command does, example
  // usage, a preview of the chat line the bot posts, and a single on/off toggle.
  // The toggle posts to ?/toggleBuiltin (built-in state lives in the modules
  // service) via the page's shared optimistic toggle handler.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
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

<div class="builtin">
  <div class="row toprow">
    <div class="row-text">
      <span class="row-label">{t('builtinInspector.enabled')}</span>
      <span class="row-help">{t('builtinInspector.enabledHelp')}</span>
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

  <div class="block">
    <span class="block-h">{t('builtinInspector.usage')}</span>
    <ul class="usage">
      {#each def.usage as u}<li><code>{u}</code></li>{/each}
    </ul>
  </div>

  <div class="block">
    <span class="block-h">{t('builtinInspector.preview')}</span>
    <div class="preview">
      <span class="bot"><Icon name="commands" size={12} /> bot</span>
      <span class="msg">{def.preview}</span>
    </div>
  </div>

  <div class="meta-row">
    <span class="meta-item">{t('builtinInspector.access')}: {PERM_LABELS[(c.perm ?? def.defaultPerm) as Perm]}</span>
    <span class="meta-item">{t('builtinInspector.cooldown')}: {c.cooldown ?? def.defaultCooldown}s</span>
  </div>
</div>

<style>
  .builtin {
    padding: 4px 2px 2px;
    font-family: var(--bb-font-body);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 4px 2px 16px;
    border-bottom: 1px solid var(--glass-border);
  }
  .row-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .row-label {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
  }
  .row-help {
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .row form {
    margin-left: auto;
  }

  .desc {
    margin: 14px 0;
    font-size: 13px;
    line-height: 1.6;
    color: var(--bb-muted);
  }

  .block {
    margin-bottom: 16px;
  }
  .block-h {
    display: block;
    margin-bottom: 8px;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
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

  .preview {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
  }
  .preview .bot {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: none;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
  }
  .preview .msg {
    font-size: 13px;
    color: var(--bb-white);
    line-height: 1.5;
  }

  .meta-row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 16px;
  }
  .meta-item {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }
</style>
