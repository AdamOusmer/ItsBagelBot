<script lang="ts">
  // Response textarea with a variable palette (insert-at-cursor), live counter,
  // and a preview line that highlights {tokens} so authors can see what the bot
  // will substitute.
  import { RESPONSE_MAX, getI18n } from '@bagel/shared';

  const i18n = getI18n();

  let {
    value = $bindable(''),
    name = 'response'
  }: {
    value: string;
    name?: string;
  } = $props();

  const TOKENS = [
    { token: '{user}', hint: 'commandEditor.tokUser' },
    { token: '{target}', hint: 'commandEditor.tokTarget' },
    { token: '{uptime}', hint: 'commandEditor.tokUptime' },
    { token: '{followage}', hint: 'commandEditor.tokFollowage' }
  ];

  let area: HTMLTextAreaElement;

  function insert(token: string) {
    const start = area?.selectionStart ?? value.length;
    const end = area?.selectionEnd ?? value.length;
    value = value.slice(0, start) + token + value.slice(end);
    // Restore focus with the caret placed after the inserted token.
    queueMicrotask(() => {
      area?.focus();
      const pos = start + token.length;
      area?.setSelectionRange(pos, pos);
    });
  }

</script>

<div class="resp-wrap">
  <textarea
    class="resp-area"
    {name}
    rows="4"
    maxlength={RESPONSE_MAX}
    placeholder={i18n.t('commandEditor.responsePlaceholder')}
    bind:value
    bind:this={area}
  ></textarea>
  <span class="resp-count">{value.length}/{RESPONSE_MAX}</span>
</div>

<div class="palette" role="toolbar" aria-label={i18n.t('commandEditor.insertVariable')}>
  {#each TOKENS as tk (tk.token)}
    <button type="button" class="var" title={i18n.t(tk.hint)} onclick={() => insert(tk.token)}>{tk.token}</button>
  {/each}
</div>

<!-- The rendered reply lives in ChatPreview (chat rehearsal), owned by the editor. -->

<style>
  .resp-wrap { position: relative; }

  .resp-area {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 96px;
    padding: 12px 14px 26px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.6;
    transition: border-color var(--bb-dur-base, 160ms) ease, box-shadow var(--bb-dur-base, 160ms) ease;
  }
  .resp-area::placeholder { color: var(--bb-muted); opacity: 0.7; }
  .resp-area:focus {
    outline: none;
    border-color: rgba(82, 183, 136, 0.5);
    box-shadow: 0 0 0 3px rgba(82, 183, 136, 0.12);
    background: rgba(255, 255, 255, 0.04);
  }

  .resp-count {
    position: absolute;
    right: 10px;
    bottom: 8px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    pointer-events: none;
    opacity: 0.7;
  }

  .palette { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .var {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid rgba(201, 168, 124, 0.22);
    border-radius: 999px;
    padding: 3px 10px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .var:hover { background: rgba(201, 168, 124, 0.18); color: var(--bb-white); }

</style>
