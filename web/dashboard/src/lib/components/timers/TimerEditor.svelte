<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Checkbox, Field, Input, Text, getI18n, type TimerDef } from '@bagel/kit';
  import { urlFetchNames, URLFETCH_TOKEN_CAP } from '@bagel/kit/engine/fetch-validate';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  const MIN_INTERVAL_MINUTES = 1;
  const MAX_INTERVAL_MINUTES = 1440;

  let {
    draft = $bindable<TimerDef>(),
    attempted = false
  }: {
    draft: TimerDef;
    attempted?: boolean;
  } = $props();

  const { t } = getI18n();

  let minutes = $state(Math.max(MIN_INTERVAL_MINUTES, Math.round(draft.intervalSeconds / 60)));
  $effect(() => {
    draft.intervalSeconds = minutes * 60;
  });

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
    (attempted || touched.interval) && !(Number.isInteger(minutes) && minutes >= MIN_INTERVAL_MINUTES && minutes <= MAX_INTERVAL_MINUTES)
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
      maxlength={500}
      bind:value={draft.message}
      invalid={!!messageError}
      describedby={messageError ? 'timer-msg-err' : undefined}
      required
      placeholder={t('timers.fieldMessagePh')}
      onblur={() => (touched.message = true)}
    />
  </Field>
  <ChatPreview kind="timer" response={draft.message} />

  <Field
    label={t('timers.fieldInterval')}
    hint={t('timers.fieldIntervalHint')}
    hintId="timer-int-help"
    error={intervalError}
    errorId="timer-int-err"
  >
    <div class="interval-row">
      <span class="num">
        <Input
          type="number"
          min={MIN_INTERVAL_MINUTES}
          max={MAX_INTERVAL_MINUTES}
          invalid={!!intervalError}
          aria-invalid={intervalError ? 'true' : undefined}
          aria-describedby={intervalError ? 'timer-int-help timer-int-err' : 'timer-int-help'}
          bind:value={minutes}
          onblur={() => (touched.interval = true)}
        />
      </span>
      <Text as="span" size="sm" tone="muted">{t('timers.unitMinutes')}</Text>
    </div>
  </Field>

  <Field label={t('timers.fieldMinChatLines')} hint={t('timers.fieldMinChatLinesHint')}>
    <span class="num"><Input type="number" min="0" max="100" bind:value={draft.minChatLines} /></span>
  </Field>

  <Field label={t('timers.fieldMaxFires')} hint={t('timers.fieldMaxFiresHint')}>
    <span class="num"><Input type="number" min="0" max="100" bind:value={draft.maxFiresPerStream} /></span>
  </Field>

  <Field label={t('timers.fieldEndsAt')} hint={t('timers.fieldEndsAtHint')}>
    <div class="ends-row">
      <Input type="datetime-local" value={endsAtLocal} oninput={onEndsAtInput} />
      {#if tzAbbr}<Text as="span" size="xs" mono tone="muted" aria-hidden="true">{tzAbbr}</Text>{/if}
    </div>
  </Field>

  <div class="check">
    <Checkbox bind:checked={draft.enabled}>{t('timers.active')}</Checkbox>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .interval-row { display: flex; align-items: center; gap: 10px; }
  .num { display: block; width: 100px; flex: none; }

  .ends-row { display: flex; align-items: center; gap: 10px; }

  .check { margin: 4px 0 6px; --bb-check-align: center; }
</style>
