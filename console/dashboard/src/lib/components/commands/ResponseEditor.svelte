<script lang="ts">
  // Response textarea with a variable palette (insert-at-cursor), live counter,
  // and a preview line that highlights {tokens} so authors can see what the bot
  // will substitute.
  //
  // maxLines > 1 turns the response newline-delimited: each line is sent as its
  // own chat message (commands allow up to 5). The default stays single-line
  // for callers whose reply is one message (module replies) — Enter is
  // swallowed and pasted newlines collapse to spaces.
  import { RESPONSE_MAX, responseLines, getI18n } from '@bagel/shared';

  const i18n = getI18n();

  // tokens: the insert palette. Defaults to the command tokens (hint = i18n key);
  // callers (e.g. module replies) can pass their own with a plain `label` title.
  type PaletteToken = { token: string; hint?: string; label?: string };

  const DEFAULT_TOKENS: PaletteToken[] = [
    { token: '{user}', hint: 'commandEditor.tokUser' },
    { token: '{target}', hint: 'commandEditor.tokTarget' },
    { token: '{uptime}', hint: 'commandEditor.tokUptime' },
    { token: '{followage}', hint: 'commandEditor.tokFollowage' }
  ];

  let {
    value = $bindable(''),
    name = 'response',
    tokens = DEFAULT_TOKENS,
    placeholder,
    maxLines = 1
  }: {
    value: string;
    name?: string;
    tokens?: PaletteToken[];
    placeholder?: string;
    maxLines?: number;
  } = $props();

  const chipTitle = (tk: PaletteToken) => (tk.hint ? i18n.t(tk.hint) : (tk.label ?? tk.token));

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

  // The lines that will actually be sent (blank lines don't count) and the
  // longest one, for the per-message character budget.
  const lines = $derived(responseLines(value));
  const longest = $derived(lines.reduce((m, l) => Math.max(m, l.length), 0));
  const overChars = $derived(longest > RESPONSE_MAX);
  const overLines = $derived(lines.length > maxLines);

  // Enter must not mint a sixth message (or any second one in single-line
  // mode). Blocked at keydown so the counter never flashes an error state the
  // form would reject anyway; pasted newlines are handled in oninput below.
  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && (maxLines === 1 || lines.length >= maxLines)) e.preventDefault();
  }

  // Single-line callers keep their one-message contract even against paste.
  function onInput() {
    if (maxLines === 1 && /[\r\n]/.test(value)) value = value.replace(/[\r\n]+/g, ' ');
  }
</script>

<div class="resp-wrap">
  <textarea
    class="resp-area"
    {name}
    rows="4"
    placeholder={placeholder ?? i18n.t('commandEditor.responsePlaceholder')}
    bind:value
    bind:this={area}
    onkeydown={onKeydown}
    oninput={onInput}
  ></textarea>
  <span class="resp-count" class:over={overChars || overLines}>
    {#if maxLines > 1}
      {i18n.t('commandEditor.linesCount', { n: String(lines.length), max: String(maxLines) })} · {longest}/{RESPONSE_MAX}
    {:else}
      {value.length}/{RESPONSE_MAX}
    {/if}
  </span>
</div>
{#if maxLines > 1}
  <small class="lines-hint">{i18n.t('commandEditor.linesHint', { max: String(maxLines) })}</small>
{/if}

<div class="palette" role="toolbar" aria-label={i18n.t('commandEditor.insertVariable')}>
  {#each tokens as tk (tk.token)}
    <button type="button" class="var" title={chipTitle(tk)} onclick={() => insert(tk.token)}>{tk.token}</button>
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
    border-radius: 8px 8px;
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
  .resp-count.over { color: #cf8a78; opacity: 1; }

  .lines-hint {
    display: block;
    margin-top: 6px;
    font-size: 11px;
    color: var(--bb-muted);
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
