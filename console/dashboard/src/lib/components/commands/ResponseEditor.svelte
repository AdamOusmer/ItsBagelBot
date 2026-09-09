<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Response editor with a variable palette (insert-at-cursor) and live
  // counters. The bound `value` is the wire format: a newline-delimited string,
  // one line per chat message.
  //
  // maxLines > 1 renders one field per message with an "Add line" button:
  // each field is one chat message the bot will send (commands allow up to 5).
  // The default stays a single field for callers whose reply is one message
  // (module replies); there pasted newlines collapse to spaces.
  import { RESPONSE_MAX, getI18n } from '@bagel/shared';
  import CounterPicker from '$lib/components/counters/CounterPicker.svelte';
  import FetchSourcePicker, { type SourceDef } from '$lib/components/commands/fetches/FetchSourcePicker.svelte';

  const i18n = getI18n();

  // tokens: the insert palette. Defaults to the command tokens (hint = i18n key);
  // callers (e.g. module replies) can pass their own with a plain `label` title.
  type PaletteToken = { token: string; hint?: string; label?: string };

  // Mirrors the set the sesame scope chain actually expands
  // (app/twitch/sesame/engine/scope): a token offered here must render in chat.
  // The positional pair is offered as {1} and {2:} rather than as an abstract
  // {n}: a chip inserts literal text, so it has to be a spelling that works
  // the moment it lands in the field.
  const DEFAULT_TOKENS: PaletteToken[] = [
    { token: '{user}', hint: 'commandEditor.tokUser' },
    { token: '{target}', hint: 'commandEditor.tokTarget' },
    { token: '{args}', hint: 'commandEditor.tokArgs' },
    { token: '{1}', hint: 'commandEditor.tokWord' },
    { token: '{2:}', hint: 'commandEditor.tokWordsFrom' },
    { token: '{channel}', hint: 'commandEditor.tokChannel' },
    { token: '{userid}', hint: 'commandEditor.tokUserId' },
    { token: '{user.login}', hint: 'commandEditor.tokUserLogin' },
    { token: '{command}', hint: 'commandEditor.tokCommand' },
    { token: '{counter:name}', hint: 'commandEditor.tokCounter' },
    { token: '{random}', hint: 'commandEditor.tokRandom' },
    { token: '{choice:a,b,c}', hint: 'commandEditor.tokChoice' },
    { token: '{math:1+1}', hint: 'commandEditor.tokMath' },
    { token: '{querystring}', hint: 'commandEditor.tokQueryString' },
    { token: '{queryescape:text}', hint: 'commandEditor.tokQueryEscape' },
    { token: '{pathescape:text}', hint: 'commandEditor.tokPathEscape' },
    { token: '{repeat:3:hi}', hint: 'commandEditor.tokRepeat' },
    { token: '{countdown:2026-12-25}', hint: 'commandEditor.tokCountdown' },
    { token: '{countup:2026-12-25}', hint: 'commandEditor.tokCountup' },
    // The viewer lookups. Each is answered by an opt-in module, so a chip
    // inserts a token that stays visible in chat while that module is off —
    // the hints name the module for exactly that reason.
    { token: '{followage}', hint: 'commandEditor.tokFollowage' },
    { token: '{accountage}', hint: 'commandEditor.tokAccountAge' },
    { token: '{points}', hint: 'commandEditor.tokPoints' },
    { token: '{pointsname}', hint: 'commandEditor.tokPointsName' },
    { token: '{watchtime}', hint: 'commandEditor.tokWatchTime' },
    // The read-only counter sits beside the bump on purpose: seeing both chips
    // is what tells a broadcaster that one of them adds 1 and the other does
    // not, which no amount of hint text on a single chip can.
    { token: '{count:deaths}', hint: 'commandEditor.tokCount' },
    // The module facts. Each is answered by an opt-in module, so a chip
    // inserts a token that stays visible in chat while that module is off —
    // the hints name the module for exactly that reason.
    { token: '{quote}', hint: 'commandEditor.tokQuote' },
    { token: '{time}', hint: 'commandEditor.tokTime' },
    { token: '{song}', hint: 'commandEditor.tokSong' },
    // The chat room. These two are the only lookups in this list with no
    // module behind them, so their hints name no module to switch on — what
    // they need saying instead is that the bot counts and draws from people
    // who have SPOKEN recently, not from everyone watching.
    { token: '{chatters}', hint: 'commandEditor.tokChatters' },
    { token: '{random.chatter}', hint: 'commandEditor.tokRandomChatter' }
  ];

  let {
    value = $bindable(''),
    name = 'response',
    tokens = DEFAULT_TOKENS,
    placeholder,
    maxLines = 1,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged
  }: {
    value: string;
    name?: string;
    tokens?: PaletteToken[];
    placeholder?: string;
    maxLines?: number;
    // The channel's saved data sources. Supplied only on the command surface:
    // module replies and rewards have no defs to pick, and the chip hides
    // itself there via pickerOn rather than rendering an empty menu.
    fetchDefs?: SourceDef[];
    fetchKeys?: { label: string }[];
    onFetchDefsChanged?: (defs: SourceDef[]) => void;
  } = $props();

  const chipTitle = (tk: PaletteToken) => (tk.hint ? i18n.t(tk.hint) : (tk.label ?? tk.token));

  // The default (command) palette swaps the static counter chip for the
  // picker, which inserts an existing counter or creates one in place.
  // Callers passing their own tokens (module replies, rewards) keep a plain
  // palette.
  const pickerOn = $derived(tokens === DEFAULT_TOKENS);
  const paletteTokens = $derived(pickerOn ? tokens.filter((tk) => !tk.token.startsWith('{counter')) : tokens);

  // One entry per message field. Seeded from the incoming value (a draft
  // restore or an edit of an existing multi-line command); from then on the
  // fields are the source of truth and `value` mirrors their join, so the form
  // post, the client validator and the chat rehearsal all see the wire format.
  // Not capped here: an over-limit value renders all its fields and the
  // validator blocks the save, rather than silently dropping content.
  let fields = $state(value.split(/\r\n|\r|\n/));
  let areas = $state<(HTMLTextAreaElement | undefined)[]>([]);
  let focused = $state(0);

  $effect(() => {
    value = fields.join('\n');
  });

  function insert(token: string) {
    const i = Math.min(focused, fields.length - 1);
    const el = areas[i];
    const cur = fields[i] ?? '';
    const start = el?.selectionStart ?? cur.length;
    const end = el?.selectionEnd ?? cur.length;
    fields[i] = cur.slice(0, start) + token + cur.slice(end);
    // Restore focus with the caret placed after the inserted token.
    queueMicrotask(() => {
      el?.focus();
      const pos = start + token.length;
      el?.setSelectionRange(pos, pos);
    });
  }

  function focusField(i: number) {
    queueMicrotask(() => areas[i]?.focus());
  }

  function addLine(after: number = fields.length - 1) {
    if (fields.length >= maxLines) return;
    fields.splice(after + 1, 0, '');
    focused = after + 1;
    focusField(focused);
  }

  function removeLine(i: number) {
    fields.splice(i, 1);
    if (fields.length === 0) fields.push('');
    focused = Math.min(i, fields.length - 1);
    focusField(focused);
  }

  // A field is one message: Enter never inserts a newline, with room left it
  // adds the next field instead; Backspace on an empty field folds it away.
  function onKeydown(e: KeyboardEvent, i: number) {
    if (e.key === 'Enter') {
      e.preventDefault();
      addLine(i);
    } else if (e.key === 'Backspace' && fields.length > 1 && fields[i] === '') {
      e.preventDefault();
      removeLine(i);
    }
  }

  // Pasted newlines distribute into fields below (up to the cap; overflow
  // folds into the last field with spaces). In single-line mode that collapses
  // to the one-message contract.
  function onInput(i: number) {
    const v = fields[i];
    if (!/[\r\n]/.test(v)) return;
    const parts = v.split(/\r\n|\r|\n/);
    const room = maxLines - fields.length;
    const keep = parts.slice(0, room + 1);
    const overflow = parts.slice(room + 1);
    if (overflow.length) keep[keep.length - 1] = [keep[keep.length - 1], ...overflow].join(' ');
    fields.splice(i, 1, ...keep);
    focused = Math.min(i + keep.length - 1, fields.length - 1);
  }

  const fieldPlaceholder = (i: number) =>
    i === 0 ? (placeholder ?? i18n.t('commandEditor.responsePlaceholder')) : i18n.t('commandEditor.linePlaceholder');
</script>

{#if maxLines > 1}
  <div class="lines" role="group" aria-label={i18n.t('commandEditor.response')}>
    {#each fields as _, i (i)}
      <div class="line-field">
        <span class="line-idx" aria-hidden="true">{i + 1}</span>
        <div class="resp-wrap slim">
          <textarea
            class="resp-area slim"
            rows="2"
            placeholder={fieldPlaceholder(i)}
            bind:value={fields[i]}
            bind:this={areas[i]}
            onfocus={() => (focused = i)}
            onkeydown={(e) => onKeydown(e, i)}
            oninput={() => onInput(i)}
          ></textarea>
          <span class="resp-count" class:over={fields[i].length > RESPONSE_MAX}>{fields[i].length}/{RESPONSE_MAX}</span>
        </div>
        {#if fields.length > 1}
          <button
            type="button"
            class="line-remove"
            title={i18n.t('commandEditor.removeLine', { n: String(i + 1) })}
            aria-label={i18n.t('commandEditor.removeLine', { n: String(i + 1) })}
            onclick={() => removeLine(i)}
          >×</button>
        {/if}
      </div>
    {/each}
  </div>
  <!-- The form posts the joined wire format; the visible fields are unnamed. -->
  <input type="hidden" {name} {value} />
  <div class="lines-foot">
    <button type="button" class="add-line" disabled={fields.length >= maxLines} onclick={() => addLine()}>
      + {i18n.t('commandEditor.addLine')}
    </button>
    <small class="lines-hint">{i18n.t('commandEditor.linesHint', { max: String(maxLines) })}</small>
  </div>
{:else}
  <div class="resp-wrap">
    <textarea
      class="resp-area"
      {name}
      rows="4"
      placeholder={fieldPlaceholder(0)}
      bind:value={fields[0]}
      bind:this={areas[0]}
      onfocus={() => (focused = 0)}
      onkeydown={(e) => onKeydown(e, 0)}
      oninput={() => onInput(0)}
    ></textarea>
    <span class="resp-count" class:over={fields[0].length > RESPONSE_MAX}>{fields[0].length}/{RESPONSE_MAX}</span>
  </div>
{/if}

<div class="palette" role="toolbar" aria-label={i18n.t('commandEditor.insertVariable')}>
  {#each paletteTokens as tk (tk.token)}
    <button type="button" class="var" title={chipTitle(tk)} onclick={() => insert(tk.token)}>{tk.token}</button>
  {/each}
  {#if pickerOn}
    <!-- Separated from the literals above: these two open a menu instead of
         inserting what their label says, so they get their own group rather
         than adding two more identical-looking pills to the same run. -->
    <span class="palette-sep" aria-hidden="true"></span>
    <CounterPicker onInsert={insert} />
    <FetchSourcePicker defs={fetchDefs} keys={fetchKeys} onInsert={insert} onDefsChanged={onFetchDefsChanged} />
  {/if}
</div>

{#if pickerOn}
  <!-- The fallback pipe has no chip of its own: it is a suffix on a variable
       already in the field, not something to insert on its own, so it is
       documented here instead. -->
  <p class="palette-note">{i18n.t('commandEditor.fallbackHint')}</p>
{/if}

<!-- The rendered reply lives in ChatPreview (chat rehearsal), owned by the editor. -->

<style>
  .resp-wrap { position: relative; flex: 1; min-width: 0; }

  .palette-note {
    margin: 6px 0 0;
    font-size: 0.78rem;
    line-height: 1.4;
    color: var(--bb-muted);
  }

  .resp-area {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 96px;
    padding: 12px 14px 26px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-sm);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.6;
    transition: border-color var(--bb-dur-base, 160ms) ease, box-shadow var(--bb-dur-base, 160ms) ease;
  }
  .resp-area.slim { min-height: 54px; padding: 9px 12px 22px; }
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

  /* --- multi-line message stack --- */
  .lines { display: flex; flex-direction: column; gap: 8px; }
  .line-field { display: flex; align-items: flex-start; gap: 8px; }

  /* Which message this field becomes, mirroring the rehearsal's send order. */
  .line-idx {
    flex: none;
    margin-top: 9px;
    width: 18px;
    height: 18px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill);
  }

  .line-remove {
    flex: none;
    margin-top: 7px;
    width: 22px;
    height: 22px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 14px;
    line-height: 1;
    color: var(--bb-muted);
    background: transparent;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill);
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) ease;
  }
  .line-remove:hover { color: #cf8a78; border-color: rgba(176, 90, 70, 0.5); }

  .lines-foot {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 8px;
    flex-wrap: wrap;
  }
  .add-line {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-green-glow, #52b788);
    background: rgba(82, 183, 136, 0.06);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    border-radius: var(--bb-radius-pill);
    padding: 4px 12px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) ease;
  }
  .add-line:hover:not(:disabled) { background: rgba(82, 183, 136, 0.14); }
  .add-line:disabled { opacity: 0.45; cursor: default; }

  .lines-hint {
    font-size: 11px;
    color: var(--bb-muted);
    opacity: 0.7;
  }

  .palette { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 8px; }
  /* Hairline between "literals you insert" and "menus you open". Collapses to
     nothing when the row wraps, so it never leaves a rule dangling on its own
     line. */
  .palette-sep {
    width: 1px;
    align-self: stretch;
    min-height: 16px;
    margin: 0 2px;
    background: var(--rule, var(--bb-border));
  }
  .var {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid rgba(201, 168, 124, 0.22);
    border-radius: var(--bb-radius-pill);
    padding: 3px 10px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .var:hover { background: rgba(201, 168, 124, 0.18); color: var(--bb-white); }

</style>
