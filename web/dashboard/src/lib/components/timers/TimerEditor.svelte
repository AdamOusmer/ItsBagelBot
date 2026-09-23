<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Timer editor fields (create + edit share it), rendered in the page's docked
  // inspector. Fields only: the page owns the <form> and the sticky EditorFooter
  // so Save/Cancel stay visible below a long form and the save is stale-safe.
  //
  // Every input is wrapped in the shared <Field> (visible, associated label). The
  // interval field states its unit (minutes) beside the control and its valid
  // range in the help line. Blurring a field or attempting Save surfaces an
  // inline error wired to the input via aria-invalid + aria-describedby; the
  // owning form performs the matching submit-time gate.
  import { getI18n, type TimerDef, Field } from '@bagel/kit';
  import { Checkbox } from '@bagel/kit';
  import { urlFetchNames, URLFETCH_TOKEN_CAP } from '@bagel/kit/engine/fetch-validate';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  // Whole minutes; mirrors the server clamp (60s–24h => 1–1440 min).
  const MIN = 1;
  const MAX = 1440;

  let {
    draft = $bindable<TimerDef>(),
    attempted = false
  }: {
    draft: TimerDef;
    /** True after the owning form rejected a submit attempt. */
    attempted?: boolean;
  } = $props();

  const { t } = getI18n();

  // The wire value is whole seconds; the field reads/writes whole minutes so
  // "every 10" reads naturally instead of "every 600".
  let minutes = $state(Math.max(MIN, Math.round(draft.intervalSeconds / 60)));
  $effect(() => {
    draft.intervalSeconds = minutes * 60;
  });

  // Errors surface only after a field is touched or Save is attempted, so a
  // fresh "new timer" form is not pre-flagged.
  let touched = $state({ message: false, interval: false });
  // urlfetchOverCap mirrors commands-validate.ts's responseProblem: distinct
  // NORMALIZED {urlfetch:...} names, not occurrences, same as the save-side
  // check (timers-parse.ts) it has to agree with — a draft that passes here
  // must pass there too, or Save fails with no field to blame.
  const urlfetchOverCap = $derived(urlFetchNames(draft.message).length > URLFETCH_TOKEN_CAP);
  const messageError = $derived(
    (attempted || touched.message) && draft.message.trim().length === 0
      ? t('timers.errMessage')
      : (attempted || touched.message) && urlfetchOverCap
        ? t('timers.errUrlfetchCap', { max: URLFETCH_TOKEN_CAP })
        : undefined
  );
  const intervalError = $derived(
    (attempted || touched.interval) && !(Number.isInteger(minutes) && minutes >= MIN && minutes <= MAX)
      ? t('timers.errInterval')
      : undefined
  );

  // Gate/stop fields (docs/specs/timer-conditions.md §7): the server clamp is
  // the source of truth (D12), so these have no client-side error gate, only
  // min/max affordances on the inputs.

  // endsAt on the wire is a UTC instant; datetime-local reads and writes
  // browser-local wall time with no offset, so it needs its own local
  // <-> ISO conversion, first use of this input type in the repo (D7).
  function isoToLocalInput(iso: string): string {
    const d = new Date(iso);
    if (!iso || Number.isNaN(d.getTime())) return '';
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }
  function localInputToIso(local: string): string {
    const d = new Date(local);
    return local && !Number.isNaN(d.getTime()) ? d.toISOString() : '';
  }

  // Seeded once from the draft, written back only from the input handler, not
  // an effect: the local form drops seconds, so an effect would rewrite a
  // stored instant on open and flip the inspector's dirty flag with no edit.
  let endsAtLocal = $state(isoToLocalInput(draft.endsAt));
  function onEndsAtInput(e: Event) {
    endsAtLocal = (e.currentTarget as HTMLInputElement).value;
    draft.endsAt = localInputToIso(endsAtLocal);
  }

  // Short zone label ("EDT", "GMT+2") shown beside the picker so the
  // broadcaster knows which zone the instant is being read in; computed once,
  // it does not need to react to anything on this page.
  const tzAbbr =
    new Intl.DateTimeFormat(undefined, { timeZoneName: 'short' })
      .formatToParts(new Date())
      .find((p) => p.type === 'timeZoneName')?.value ?? '';
</script>

<div class="editor">
  <Field label={t('timers.fieldMessage')} error={messageError} errorId="timer-msg-err">
    <ResponseEditor
      surface="timer"
      name="message"
      maxlength={500}
      bind:value={draft.message}
      invalid={!!messageError}
      describedby={messageError ? 'timer-msg-err' : undefined}
      required
      placeholder={t('timers.fieldMessagePh')}
      onblur={() => (touched.message = true)}
    />
  </Field>
  <!-- kind="timer": Go's timerChain mirror (rehearseTimer) — no viewer line,
       a tick has nobody typing it. -->
  <ChatPreview kind="timer" response={draft.message} />

  <Field label={t('timers.fieldInterval')} error={intervalError} errorId="timer-int-err">
    <div class="interval-row">
      <input
        class="bb-input num"
        type="number"
        min={MIN}
        max={MAX}
        data-invalid={intervalError ? '' : undefined}
        aria-invalid={intervalError ? 'true' : undefined}
        aria-describedby={intervalError ? 'timer-int-help timer-int-err' : 'timer-int-help'}
        bind:value={minutes}
        onblur={() => (touched.interval = true)}
      />
      <span class="unit">{t('timers.unitMinutes')}</span>
    </div>
    <small id="timer-int-help" class="help">{t('timers.fieldIntervalHint')}</small>
  </Field>

  <Field label={t('timers.fieldMinChatLines')}>
    <input class="bb-input num" type="number" min="0" max="100" bind:value={draft.minChatLines} />
    <small class="help">{t('timers.fieldMinChatLinesHint')}</small>
  </Field>

  <Field label={t('timers.fieldMaxFires')}>
    <input class="bb-input num" type="number" min="0" max="100" bind:value={draft.maxFiresPerStream} />
    <small class="help">{t('timers.fieldMaxFiresHint')}</small>
  </Field>

  <Field label={t('timers.fieldEndsAt')}>
    <div class="ends-row">
      <input class="bb-input" type="datetime-local" value={endsAtLocal} oninput={onEndsAtInput} />
      {#if tzAbbr}<span class="tz" aria-hidden="true">{tzAbbr}</span>{/if}
    </div>
    <small class="help">{t('timers.fieldEndsAtHint')}</small>
  </Field>

  <div class="check">
    <Checkbox bind:checked={draft.enabled}>{t('timers.active')}</Checkbox>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .help { color: var(--bb-muted); opacity: 0.7; font-size: 11px; display: block; margin-top: 2px; }

  .interval-row { display: flex; align-items: center; gap: 10px; }
  /* Extra specificity so the fixed width wins over Field's `.bb-input { width: 100% }`. */
  .editor .interval-row .num { width: 100px; flex: none; }
  .editor .num { width: 100px; flex: none; }
  .unit { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  .ends-row { display: flex; align-items: center; gap: 10px; }
  .tz { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); white-space: nowrap; }

  .check { margin: 4px 0 6px; --bb-check-align: center; }
</style>
