<script lang="ts">
  // Timer editor fields (create + edit share it), rendered in the page's docked
  // inspector. Fields only: the page owns the <form> and the sticky EditorFooter
  // so Save/Cancel stay visible below a long form and the save is stale-safe.
  //
  // Every input is wrapped in the shared <Field> (visible, associated label). The
  // interval field states its unit (minutes) beside the control and its valid
  // range in the help line. Blurring a field surfaces an inline error that is
  // wired to the input via aria-invalid + aria-describedby; the page's own
  // canSave still gates the actual submit, so this is display-only.
  import { getI18n, type TimerDef, Field, FieldError } from '@bagel/shared';
  import CheckButton from '$lib/components/CheckButton.svelte';

  // Whole minutes; mirrors the server clamp (60s–24h => 1–1440 min).
  const MIN = 1;
  const MAX = 1440;

  let {
    draft = $bindable<TimerDef>()
  }: {
    draft: TimerDef;
  } = $props();

  const { t } = getI18n();

  // The wire value is whole seconds; the field reads/writes whole minutes so
  // "every 10" reads naturally instead of "every 600".
  let minutes = $state(Math.max(MIN, Math.round(draft.intervalSeconds / 60)));
  $effect(() => {
    draft.intervalSeconds = minutes * 60;
  });

  // Errors surface only after a field is touched, so a fresh "new timer" form is
  // not pre-flagged. Display-only: the page's canSave owns the real save gate.
  let touched = $state({ message: false, interval: false });
  const messageError = $derived(
    touched.message && draft.message.trim().length === 0 ? t('timers.errMessage') : undefined
  );
  const intervalError = $derived(
    touched.interval && !(Number.isInteger(minutes) && minutes >= MIN && minutes <= MAX)
      ? t('timers.errInterval')
      : undefined
  );
</script>

<div class="editor">
  <Field label={t('timers.fieldMessage')}>
    <textarea
      class="search msg-area"
      placeholder={t('timers.fieldMessagePh')}
      maxlength="500"
      rows="3"
      required
      aria-invalid={messageError ? 'true' : undefined}
      aria-describedby={messageError ? 'timer-msg-err' : undefined}
      bind:value={draft.message}
      onblur={() => (touched.message = true)}
    ></textarea>
    <span id="timer-msg-err"><FieldError message={messageError} /></span>
  </Field>

  <Field label={t('timers.fieldInterval')}>
    <div class="interval-row">
      <input
        class="search num"
        type="number"
        min={MIN}
        max={MAX}
        aria-invalid={intervalError ? 'true' : undefined}
        aria-describedby={intervalError ? 'timer-int-help timer-int-err' : 'timer-int-help'}
        bind:value={minutes}
        onblur={() => (touched.interval = true)}
      />
      <span class="unit">{t('timers.unitMinutes')}</span>
    </div>
    <small id="timer-int-help" class="help">{t('timers.fieldIntervalHint')}</small>
    <span id="timer-int-err"><FieldError message={intervalError} /></span>
  </Field>

  <div class="check">
    <CheckButton bind:checked={draft.enabled} label={t('timers.active')} />
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .help { color: var(--bb-muted); opacity: 0.7; font-size: 11px; display: block; margin-top: 2px; }

  .msg-area {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    resize: vertical;
    min-height: 64px;
    padding: 9px 11px;
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.35);
    color: var(--bb-white);
  }

  .interval-row { display: flex; align-items: center; gap: 10px; }
  /* Extra specificity so the fixed width wins over Field's `.search { width:100% }`. */
  .editor .interval-row .num { width: 100px; flex: none; }
  .unit { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  .check { margin: 4px 0 6px; }
  .check :global(.cb) { align-items: center; }
</style>
