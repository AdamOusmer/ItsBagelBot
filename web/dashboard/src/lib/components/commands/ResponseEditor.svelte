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
  import { RESPONSE_MAX, getI18n, Chip, Textarea } from '@bagel/kit';
  import { pickCommonTokens } from '@bagel/kit/engine/common-tokens';
  import { chipsFor } from '@bagel/kit/variables';
  import CounterPicker from '$lib/components/counters/CounterPicker.svelte';
  import FetchSourcePicker, { type SourceDef } from '$lib/components/commands/fetches/FetchSourcePicker.svelte';

  const i18n = getI18n();

  // tokens: the insert palette. Defaults to the command tokens (hint = i18n key);
  // callers (e.g. module replies) can pass their own with a plain `label` title.
  type PaletteToken = { token: string; hint?: string; label?: string };

  // Derived from the manifest (docs/specs/variables-catalog.md D5, D8) rather
  // than hand-kept: chipsFor('custom') is the first form of every Variable
  // this surface offers, in the order the guide page uses too, plus the one
  // extra form flagged chipHint ({2:}). A chip inserts literal text, so each
  // spelling has to work the moment it lands in the field.
  //
  // {counter} and {urlfetch} are filtered out below (paletteTokens): each has
  // its own picker (CounterPicker, FetchSourcePicker) that inserts a real
  // name instead of a literal placeholder, so a bare {counter:name} or
  // {urlfetch:weather} chip would invite a broadcaster to ship a token that
  // resolves to nothing.
  const DEFAULT_TOKENS: PaletteToken[] = chipsFor('custom').map((c) => ({ token: c.token, hint: c.hintKey }));

  let {
    value = $bindable(''),
    name = 'response',
    tokens = DEFAULT_TOKENS,
    placeholder,
    invalid = false,
    describedby,
    required = false,
    maxLines = 1,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged
  }: {
    value: string;
    name?: string;
    tokens?: PaletteToken[];
    placeholder?: string;
    /** Validation state supplied by the form that owns this editor. */
    invalid?: boolean;
    /** Id(s) of help or error text associated with the textarea. */
    describedby?: string;
    required?: boolean;
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
  const paletteTokens = $derived(
    pickerOn ? tokens.filter((tk) => !tk.token.startsWith('{counter') && !tk.token.startsWith('{urlfetch')) : tokens
  );

  // The command palette is FIVE chips and nothing else. The whole catalog
  // used to render at once (forty chips), then eight chips plus a ghost "More
  // variables" toggle that expanded the rest in place; both put the wall back
  // in front of a first-time reader, the second one click in. Which five is
  // @bagel/kit/engine/common-tokens, shared with the marketing command builder
  // so the two rehearsal surfaces open on the same set, and the reason the
  // toggle is not coming back is recorded there. The rest of the catalog is
  // real and documented and belongs on another surface.
  //
  // A caller passing its OWN tokens (module replies, rewards) is never
  // truncated: those catalogs are already short (three to eight entries) and
  // dropping two of five with nowhere to see them loses content to save
  // nothing.
  const shownTokens = $derived(pickerOn ? pickCommonTokens(paletteTokens, (tk) => tk.token) : paletteTokens);

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

  // Textarea forwards native focus events, so the editor can retain its caret
  // target without adding a dashboard-specific element-ref prop to the shared
  // design-system component.
  function rememberArea(event: FocusEvent, i: number) {
    if (event.currentTarget instanceof HTMLTextAreaElement) areas[i] = event.currentTarget;
    focused = i;
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
          <Textarea
            class="resp-area slim"
            rows={2}
            fill
            {invalid}
            placeholder={fieldPlaceholder(i)}
            aria-invalid={invalid ? 'true' : undefined}
            aria-describedby={describedby}
            {required}
            bind:value={fields[i]}
            onfocus={(e: FocusEvent) => rememberArea(e, i)}
            onkeydown={(e: KeyboardEvent) => onKeydown(e, i)}
            oninput={() => onInput(i)}
          />
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
    <Textarea
      class="resp-area"
      {name}
      rows={4}
      fill
      {invalid}
      placeholder={fieldPlaceholder(0)}
      aria-invalid={invalid ? 'true' : undefined}
      aria-describedby={describedby}
      {required}
      bind:value={fields[0]}
      onfocus={(e: FocusEvent) => rememberArea(e, 0)}
      onkeydown={(e: KeyboardEvent) => onKeydown(e, 0)}
      oninput={() => onInput(0)}
    />
    <span class="resp-count" class:over={fields[0].length > RESPONSE_MAX}>{fields[0].length}/{RESPONSE_MAX}</span>
  </div>
{/if}

<div class="palette" role="toolbar" aria-label={i18n.t('commandEditor.insertVariable')}>
  {#each shownTokens as tk (tk.token)}
    <Chip tone="muted" title={chipTitle(tk)} onclick={() => insert(tk.token)}>{tk.token}</Chip>
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

  /* Textarea owns the frame, focus and invalid highlight. These two knobs only
     reserve room for the per-message counter inside that shared control. */
  :global(.resp-area textarea) { min-height: 96px; padding-bottom: 16px; }
  :global(.resp-area.slim textarea) { min-height: 54px; padding-bottom: 12px; }

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

</style>
