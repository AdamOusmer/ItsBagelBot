<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // The ONE variable palette every response editor renders: a fixed chip row
  // (pinned chips, plus the counter/fetch pickers on the command surface) and
  // an "All variables" trigger that opens the full catalog in a PickerPanel
  // sheet. Replaces eight editors that each rendered their own flat chip
  // strip (ResponseEditor's hand-kept DEFAULT_TOKENS, BuiltinInspector/
  // ReplyEditor/TriggerRuleEditor/RewardEditor/Spotify/Govee/TimerEditor's own
  // `palette` derivations) with one component reading @bagel/kit/variables'
  // pinnedFor/sheetFor(surface).
  //
  // The sheet is a PickerPanel — portalled and position:fixed — rather than an
  // in-flow expander, because ResponseEditor's textarea sits inside
  // InspectorSurface's `overflow: hidden` scroller: an in-flow "show more"
  // block pushes that scroller's content down and the docked Save/Cancel
  // footer with it, which is the vertical layout shift this app's own rule
  // (no vertical motion in a form) forbids. Opening the sheet must not move
  // the textarea's screen position by one pixel: measured directly
  // (getBoundingClientRect().top on ResponseEditor's textarea, before vs.
  // after opening) at 378.421875 both times at desktop width and 285.953125
  // both times at 375px — recorded here, not only in a PR description, so
  // the next person touching this file has the actual numbers a regression
  // would move.
  import { getI18n, Chip, PickerPanel, SearchInput, Tag, moduleDef, builtinDef } from '@bagel/kit';
  import { pinnedFor, sheetFor, type VariableChip, type VariableGroup, type VariableSurface } from '@bagel/kit/variables';
  import { webHref } from '@bagel/kit/site-links';
  import CounterPicker from '$lib/components/counters/CounterPicker.svelte';
  import FetchSourcePicker, { type SourceDef } from '$lib/components/commands/fetches/FetchSourcePicker.svelte';

  const { t, locale } = getI18n();

  let {
    surface,
    moduleFlags = {},
    insert,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged
  }: {
    surface: VariableSurface;
    /** Live module on/off state (module-flags.ts), for the "Requires X ·
     * Off" tag. Absent (a caller with no layout data, e.g. a test harness)
     * renders every "Requires" tag with no "Off" beside it, never a wrong one. */
    moduleFlags?: Record<string, boolean>;
    insert: (token: string) => void;
    // Forwarded to FetchSourcePicker; only meaningful when surface === 'custom'
    // (module replies and rewards have no data sources to pick).
    fetchDefs?: SourceDef[];
    fetchKeys?: { label: string }[];
    onFetchDefsChanged?: (defs: SourceDef[]) => void;
  } = $props();

  const isCustom = $derived(surface === 'custom');

  // The five guide/chip groups, in the order the guide page and this sheet
  // both use (docs/specs/variables-catalog.md phase 4).
  const GROUPS: readonly VariableGroup[] = ['who', 'typed', 'stream', 'fun', 'data'];

  // The chip ROW: pinnedFor already caps it at six and knows the one surface
  // (triggers) whose row is not simply "every VariableDef.pinned entry" — see
  // its own comment in surfaces.ts.
  const pinnedChips = $derived(pinnedFor(surface));

  // The SHEET: sheetFor is a superset of the chip row's own chipsFor (see its
  // comment in surfaces.ts) — it adds back the manifest context every reply
  // chain shares (Who/Fun) and drops what a dedicated picker already owns
  // (custom's counter/urlfetch). Reply-only tokens (a module/reward/builtin's
  // own ReplyTokens) carry no `group`, so a simple boolean split sorts them
  // into "This reply" versus the five ordinary group sections.
  const sheetChips = $derived(sheetFor(surface));
  const replyChips = $derived(sheetChips.filter((c) => c.replyOnly));
  const manifestChips = $derived(sheetChips.filter((c) => !c.replyOnly));
  // Only a group with at least one STRUCTURAL row renders a header at all —
  // computed off the unfiltered set, never off the search results, so typing
  // in the box can empty a section's rows (which stays rendered, showing
  // "Nothing here yet.") without ever making a whole header appear or
  // disappear. A surface whose sheet is built entirely from its own reply
  // tokens plus REPLY_MANIFEST_CHIPS (see surfaces.ts) only ever touches Who
  // and Fun, so Typed/Stream/Data are never rendered for it at all.
  const structuralGroups = $derived(GROUPS.filter((g) => manifestChips.some((c) => c.group === g)));
  // Nothing to show at all (no reply tokens and no manifest chips): hides the
  // "All variables" trigger rather than opening a sheet with nothing in it.
  // Never true for a shipped surface today (every reply-shaped one gets
  // REPLY_MANIFEST_CHIPS at minimum), kept for a future surface that could.
  const sheetEmpty = $derived(sheetChips.length === 0);

  let open = $state(false);
  let btnEl = $state<HTMLButtonElement>();
  let listEl = $state<HTMLDivElement>();
  let searchEl = $state<HTMLInputElement>();
  // searchValue is the box's own text (updates every keystroke, for display);
  // query is what the sections filter on, debounced by SearchInput's own
  // oninput callback (debounceMs={120}) rather than by binding `value`
  // directly, which would update in lockstep with the box and make the
  // debounce a no-op.
  let searchValue = $state('');
  let query = $state('');
  let focusedId = $state<string | null>(null);

  $effect(() => {
    if (!open) return;
    // Sheet content (search + rows) is portalled and mounts async relative to
    // `open` flipping true; a microtask is enough to land after it without
    // guessing at a frame.
    queueMicrotask(() => searchEl?.focus());
  });

  function toggle() {
    open = !open;
    if (open) {
      searchValue = '';
      query = '';
      focusedId = null;
    }
  }

  // The picker's onClose: Escape, an outside click, or the mobile scrim tap —
  // every path that closes the sheet WITHOUT picking anything. None of those
  // hands focus anywhere on its own: the desktop dropdown carries no
  // `use:trapFocus` at all, and the mobile sheet's trap only holds focus
  // WHILE open, it does not restore it on close. So this is the one place
  // that has to put focus back on the trigger explicitly — pick() (below)
  // deliberately does not call this, since it wants the opposite outcome.
  function closeSheet() {
    open = false;
    btnEl?.focus();
  }

  // Clicking a row both inserts (this IS the point of a picker: one click,
  // not "select then confirm") and closes, matching CounterPicker/
  // FetchSourcePicker's own pick(). This sets `open = false` directly rather
  // than calling closeSheet(): closeSheet's whole job is restoring focus to
  // the trigger, and here the opposite is wanted — insert() (ResponseEditor's)
  // restores focus to the textarea itself via its own queued microtask, and
  // that has to be the LAST word on where focus ends up. Closing BEFORE
  // inserting matters for the same reason: on the mobile sheet, closing tears
  // down `use:trapFocus`, whose own teardown can touch focus synchronously in
  // this same tick; doing that first means insert()'s later-queued microtask
  // is what runs last and wins, landing focus on the textarea rather than
  // stranding it wherever the trap's teardown left it.
  function pick(chip: VariableChip) {
    open = false;
    insert(chip.token);
  }

  /** A module's or built-in's display label for the "Requires X" tag — the
   * one place a Variable's `requires` (a bare catalog id) becomes copy a
   * broadcaster reads, so it has to check both catalogs: `requires` names an
   * opt-in module (quotes, time, songqueue, loyalty) OR a built-in command
   * (followage, accountage, uptime, title, game), and nothing else tells the
   * two apart at this point but which catalog answers. */
  function requiresLabel(id: string): string {
    return moduleDef(id)?.label ?? builtinDef(id)?.label ?? id;
  }

  // A row with no locale hint (a ReplyToken the catalog hasn't given its own
  // copy yet) falls back to "{token} → sample" — ReplyToken.sample, carried
  // on the chip for exactly this — rather than showing a bare token with no
  // explanation at all, the same fallback ResponseEditor's old chipTitle used.
  function rowHint(c: VariableChip): string | undefined {
    if (c.hintKey) return t(c.hintKey);
    return c.sample ? `${c.token} → ${c.sample}` : undefined;
  }

  function matches(c: VariableChip, q: string): boolean {
    if (!q) return true;
    const needle = q.toLowerCase();
    if (c.token.toLowerCase().includes(needle)) return true;
    const hint = rowHint(c);
    return hint ? hint.toLowerCase().includes(needle) : false;
  }

  const filteredReply = $derived(replyChips.filter((c) => matches(c, query)));
  const groupRows = $derived(
    structuralGroups.map((group) => ({
      group,
      chips: manifestChips.filter((c) => c.group === group && matches(c, query))
    }))
  );

  // Roving focus over the sheet's rows by DOM query rather than a tracked
  // index: the row list is built from two independently-filtered arrays
  // (reply + five groups) re-rendered by #each, so "the Nth row" has no
  // stable index to bind an element array to across a keystroke. Querying
  // the rendered buttons in document order is exactly what ArrowUp/Down
  // needs and nothing more.
  function rowEls(): HTMLButtonElement[] {
    return Array.from(listEl?.querySelectorAll<HTMLButtonElement>('button.row') ?? []);
  }

  function focusRowAt(index: number) {
    const rows = rowEls();
    if (rows.length === 0) return;
    // A negative index counts from the end (End = -1), same convention
    // Array.prototype.at uses.
    const i = index < 0 ? rows.length + index : index;
    rows[Math.max(0, Math.min(rows.length - 1, i))]?.focus();
  }

  function moveFocus(step: 1 | -1) {
    const rows = rowEls();
    if (rows.length === 0) return;
    const cur = rows.indexOf(document.activeElement as HTMLButtonElement);
    // Not on a row yet (cur === -1, e.g. arriving from the search box):
    // ArrowDown lands on row 0, never row 1 — there is no "current" row to
    // step past.
    focusRowAt(cur === -1 ? 0 : Math.min(rows.length - 1, Math.max(0, cur + step)));
  }

  // One lookup rather than a chain of key comparisons (a nested ternary once
  // sat here): each entry is exactly "the key" -> "what it does to the row
  // list", so adding or auditing a binding is a one-line diff.
  const ROW_KEYS: Readonly<Record<string, () => void>> = {
    ArrowDown: () => moveFocus(1),
    ArrowUp: () => moveFocus(-1),
    Home: () => focusRowAt(0),
    End: () => focusRowAt(-1)
  };

  function onSheetKeydown(e: KeyboardEvent) {
    // While the search box itself has focus, only ArrowDown is ours (drop
    // into the list, at row 0): Home/End/ArrowUp are the box's OWN caret
    // motion (jump to the start/end of the typed text, or do nothing) and
    // must reach the input, not get hijacked into moving a selection that
    // is not showing yet.
    if (document.activeElement instanceof HTMLInputElement) {
      if (e.key !== 'ArrowDown') return;
      e.preventDefault();
      focusRowAt(0);
      return;
    }
    const action = ROW_KEYS[e.key];
    if (!action) return;
    e.preventDefault();
    action();
  }

  const fullReferenceHref = $derived(`${webHref(locale, '/guides/variables')}${focusedId ? `#${focusedId}` : ''}`);
</script>

{#snippet rowTag(c: VariableChip)}
  {#if c.requires}
    <Tag tone="quiet">{t('commandEditor.requires', { module: requiresLabel(c.requires) })}</Tag>
    <!-- "Off" is information, not a lock: a broadcaster writing a response
         BEFORE switching the module on is the common order (they are
         building the reply first), so the row still inserts on click. The
         chat rehearsal below already shows the token staying literal while
         the module is off; blocking the click here would only be a second,
         redundant way to say the same thing, with no way to try the token
         once the module IS on without reopening the sheet. -->
    {#if moduleFlags[c.requires] === false}
      <Tag tone="error">{t('commandEditor.off')}</Tag>
    {/if}
  {/if}
{/snippet}

{#snippet row(c: VariableChip)}
  <li>
    <!-- Not the Chip component here: a Chip is a <button>, and this whole row
         is already one (matches CounterPicker/FetchSourcePicker's own list
         rows) — nesting a button in a button is invalid markup. The token
         wears the same `.bb-chip` frame as a plain styled span instead. -->
    <button
      type="button"
      class="row"
      onclick={() => pick(c)}
      onfocus={() => (focusedId = c.id ?? null)}
    >
      <span class="bb-chip bb-chip--muted row-token">{c.token}</span>
      {#if rowHint(c)}<span class="row-hint">{rowHint(c)}</span>{/if}
      {@render rowTag(c)}
    </button>
  </li>
{/snippet}

<div class="vp">
  <div class="vp-row">
    <!-- The scrollable half: pinned chips + the two pickers. Always
         overflow-x, not only under 640px — at the inspector's own width
         (~364px, InspectorSurface's docked column) six chips plus two picker
         buttons already overflow a fixed row on every viewport, desktop
         included; a media query that only kicked in on phones left the two
         pickers and however many chips didn't fit clipped and unreachable
         above 640px, with no way to scroll to them. -->
    <div class="vp-scroll">
      {#each pinnedChips as c (c.token)}
        <Chip tone="muted" title={rowHint(c) ?? c.token} onclick={() => insert(c.token)}>{c.token}</Chip>
      {/each}

      {#if isCustom}
        <span class="vp-sep" aria-hidden="true"></span>
        <CounterPicker onInsert={insert} />
        <FetchSourcePicker defs={fetchDefs} keys={fetchKeys} onInsert={insert} onDefsChanged={onFetchDefsChanged} />
      {/if}
    </div>

    {#if !sheetEmpty}
      <!-- Pinned OUTSIDE .vp-scroll, never scrolled: it is the one control on
           this row that must stay reachable without first scrolling past
           everything else, since it is how a broadcaster reaches every chip
           the row itself has no room for. Not the Chip component: it needs
           its own DOM ref to anchor the PickerPanel, which Chip.svelte does
           not forward (same reason CounterPicker/FetchSourcePicker hand-roll
           their own trigger button on the shared `.bb-chip` classes instead
           of using Chip). -->
      <button
        type="button"
        class="bb-chip bb-chip--muted vp-all"
        aria-haspopup="dialog"
        aria-expanded={open}
        onclick={toggle}
        bind:this={btnEl}
      >
        {t('commandEditor.allVariables')}
      </button>
    {/if}
  </div>

  <PickerPanel {open} anchor={btnEl} label={t('commandEditor.allVariables')} width={380} maxHeight={480} onClose={closeSheet}>
    {#snippet children()}
      <!-- role="toolbar": the WAI-ARIA shape for "a container of controls with
           its own Arrow/Home/End navigation" is exactly this sheet (search +
           rows), and it is also the one non-interactive role svelte-check
           accepts a keydown listener on (a11y_no_noninteractive_element_
           interactions) — "group" does not count as interactive enough. -->
      <div class="sheet" role="toolbar" tabindex="-1" aria-label={t('commandEditor.allVariables')} onkeydown={onSheetKeydown}>
        <SearchInput
          fill
          debounceMs={120}
          bind:value={searchValue}
          bind:element={searchEl}
          oninput={(v) => (query = v)}
          placeholder={t('commandEditor.allVariables')}
        />

        <div class="sheet-scroll" bind:this={listEl}>
          {#if replyChips.length > 0}
            <section class="sec">
              <h3 class="sec-head">{t('commandEditor.thisReply')}</h3>
              {#if filteredReply.length === 0}
                <p class="sec-empty">{t('commandEditor.noneHere')}</p>
              {:else}
                <ul class="rows">{#each filteredReply as c (c.token)}{@render row(c)}{/each}</ul>
              {/if}
            </section>
          {/if}

          {#each groupRows as { group, chips } (group)}
            <section class="sec">
              <h3 class="sec-head">{t(`vars.group.${group}.label`)}</h3>
              {#if chips.length === 0}
                <p class="sec-empty">{t('commandEditor.noneHere')}</p>
              {:else}
                <ul class="rows">{#each chips as c (c.token)}{@render row(c)}{/each}</ul>
              {/if}
            </section>
          {/each}
        </div>

        <div class="sheet-foot">
          <p class="fallback">{t('commandEditor.fallbackHint')}</p>
          <a class="full-ref" href={fullReferenceHref} target="_blank" rel="noopener">{t('commandEditor.fullReference')}</a>
        </div>
      </div>
    {/snippet}
  </PickerPanel>
</div>

<style>
  .vp { display: contents; }

  /* Fixed height so opening the sheet (a portalled overlay) can never move
     this row: it must stay exactly this tall whether it holds zero chips (a
     filtered-empty state, never real today) or six plus both pickers.
     27.5px = `.bb-chip--muted`'s own measured control height (ui/styles/
     tags.css: 11.5px line-height 1, + 7px top/bottom padding * 2, + 1px
     border * 2 = 11.5 + 14 + 2), spelled as that sum rather than a bare
     literal so the arithmetic is checkable against the control it is tied
     to instead of trusted on faith (an earlier version of this comment said
     30px, which is neither the sum above nor anything else in tags.css). */
  .vp-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: calc(11.5px + 2 * 7px + 2 * 1px);
    margin-top: 8px;
  }

  /* The scrollable half only — see the markup comment on why "All variables"
     sits outside this element. overflow-x is unconditional (not a <640px
     media query): the inspector's own column is ~364px wide on every
     viewport, desktop included, so six chips plus two pickers already need
     to scroll there, not only on a phone. flex:none on every child (chips,
     the separator, both pickers) stops them from shrinking to fit, which is
     what let a long token ({if:touser:hi there:hi everyone}) wrap onto a
     second line and grow the row instead of just scrolling past. */
  .vp-scroll {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1 1 auto;
    min-width: 0;
    overflow-x: auto;
    overflow-y: hidden;
    scroll-snap-type: x proximity;
  }
  .vp-scroll :global(> *) {
    flex: none;
    scroll-snap-align: start;
  }
  /* Scoped to this row rather than added to the shared `.bb-chip` contract:
     every other consumer of that control is a single chip in an ordinary
     wrapping flex row, where wrapping long text is the right behaviour: this
     is the one place a chip sits in a horizontally-scrolling strip instead,
     where wrapping just grows the row's height for no reason (see above). */
  .vp-scroll :global(.bb-chip) { white-space: nowrap; }

  /* Never scrolled, never shrunk: see the markup comment above it. */
  .vp-all { flex: none; }

  .vp-sep {
    width: 1px;
    align-self: stretch;
    min-height: 16px;
    margin: 0 2px;
    background: var(--rule, var(--bb-border));
    flex: none;
  }

  .sheet { display: flex; flex-direction: column; gap: 10px; min-height: 0; }

  .sheet-scroll {
    display: flex;
    flex-direction: column;
    gap: 14px;
    overflow-y: auto;
    max-height: 340px;
    padding-right: 2px;
  }

  .sec-head {
    margin: 0 0 6px;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .sec-empty {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 12px;
    font-style: italic;
    color: var(--bb-muted);
  }

  .rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .row {
    width: 100%;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding: 6px 8px;
    background: transparent;
    border: none;
    border-radius: var(--bb-radius-sm);
    cursor: pointer;
    text-align: left;
  }
  .row:hover, .row:focus-visible { background: var(--glass-fill-2); }
  .row-token { pointer-events: none; }
  .row-hint {
    flex: 1;
    min-width: 80px;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
  }

  .sheet-foot {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--rule, var(--bb-border));
  }
  .fallback { margin: 0; font-family: var(--bb-font-body); font-size: 11.5px; line-height: 1.4; color: var(--bb-muted); }
  .full-ref {
    align-self: flex-start;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-green-glow, #52b788);
    text-decoration: underline;
  }
</style>
