<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { getI18n, type TimerDef, Field } from '@bagel/kit';
  import { Checkbox } from '@bagel/kit';
  import { urlFetchNames, URLFETCH_TOKEN_CAP } from '@bagel/kit/engine/fetch-validate';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  import DurationField from '$lib/components/shared/DurationField.svelte';

  const MIN_INTERVAL_SECONDS = 60;
  const MAX_INTERVAL_SECONDS = 86_400;
  const MESSAGE_MAX = 500;

  let {
    draft = $bindable<TimerDef>(),
    attempted = false
  }: {
    draft: TimerDef;
    attempted?: boolean;
  } = $props();

  const { t } = getI18n();

  let touched = $state({ message: false, interval: false });
  const urlfetchOverCap = $derived(urlFetchNames(draft.message).length > URLFETCH_TOKEN_CAP);
  const messageError = $derived(
    (attempted || touched.message) && draft.message.trim().length === 0
      ? t('timers.errMessage')
      : (attempted || touched.message) && urlfetchOverCap
        ? t('timers.errUrlfetchCap', { max: URLFETCH_TOKEN_CAP })
        : undefined
  );
  const intervalError = $derived(
    (attempted || touched.interval) &&
    !(Number.isInteger(draft.intervalSeconds) && draft.intervalSeconds >= MIN_INTERVAL_SECONDS && draft.intervalSeconds <= MAX_INTERVAL_SECONDS)
      ? t('timers.errInterval')
      : undefined
  );

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

  let endsAtLocal = $state(isoToLocalInput(draft.endsAt));
  function onEndsAtInput(e: Event) {
    endsAtLocal = (e.currentTarget as HTMLInputElement).value;
    draft.endsAt = localInputToIso(endsAtLocal);
  }

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
      maxlength={MESSAGE_MAX}
      bind:value={draft.message}
      invalid={!!messageError}
      describedby={messageError ? 'timer-msg-err' : undefined}
      required
      placeholder={t('timers.fieldMessagePh')}
      onblur={() => (touched.message = true)}
    />
    <small class="counter">{draft.message.length}/{MESSAGE_MAX}</small>
  </Field>
  <ChatPreview kind="timer" response={draft.message} />

  <Field label={t('timers.fieldInterval')} error={intervalError} errorId="timer-int-err">
    <DurationField
      bind:value={draft.intervalSeconds}
      min={MIN_INTERVAL_SECONDS}
      max={MAX_INTERVAL_SECONDS}
      label={t('timers.fieldInterval')}
      invalid={!!intervalError}
      describedby={intervalError ? 'timer-int-help timer-int-err' : 'timer-int-help'}
      onblur={() => (touched.interval = true)}
    >
      {#snippet hint()}<span id="timer-int-help">{t('timers.fieldIntervalHint')}</span>{/snippet}
    </DurationField>
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

  .editor .num { width: 100px; flex: none; }
  .counter { display: block; text-align: right; color: var(--bb-muted); opacity: 0.7; font-size: 11px; margin-top: 4px; }

  .ends-row { display: flex; align-items: center; gap: 10px; }
  .tz { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); white-space: nowrap; }

  .check { margin: 4px 0 6px; --bb-check-align: center; }
</style>
