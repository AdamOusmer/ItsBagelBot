<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { DEFAULT_CHAT_LINES, getI18n, type TimerDef } from '@bagel/kit';
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

  const chatGateOn = $derived(draft.minChatLines > 0);
  let rememberedLines = 0;
  function setChatGate(on: boolean) {
    if (on) {
      draft.minChatLines = rememberedLines || DEFAULT_CHAT_LINES;
      return;
    }
    rememberedLines = draft.minChatLines;
    draft.minChatLines = 0;
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
    <span class="counter"><Text as="small" size="xs" tone="muted">{draft.message.length}/{MESSAGE_MAX}</Text></span>
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

  <div class="toggle">
    <SwitchRow
      control="end"
      checked={chatGateOn}
      onCheckedChange={setChatGate}
      label={t('timers.fieldChatGate')}
      hint={t('timers.fieldChatGateHint')}
      hintId="timer-chatgate-desc"
    />
  </div>

  <Field label={t('timers.fieldMinChatLines')} hint={t('timers.fieldMinChatLinesHint')}>
    <span class="num"><Input type="number" min={1} max={100} disabled={!chatGateOn} bind:value={draft.minChatLines} /></span>
  </Field>

  <Field label={t('timers.fieldChatWindow')} hint={t('timers.fieldChatWindowHint')}>
    <span class="num"><Input type="number" min={1} max={60} disabled={!chatGateOn} bind:value={draft.chatWindowMinutes} /></span>
  </Field>

  <Field label={t('timers.fieldMaxFires')} hint={t('timers.fieldMaxFiresHint')}>
    <span class="num"><Input type="number" min={0} max={100} bind:value={draft.maxFiresPerStream} /></span>
  </Field>

  <Field label={t('timers.fieldEndsAt')} hint={t('timers.fieldEndsAtHint')}>
    <div class="ends-row">
      <Input type="datetime-local" value={endsAtLocal} oninput={onEndsAtInput} />
      {#if tzAbbr}<Text as="span" size="xs" mono tone="muted" aria-hidden="true">{tzAbbr}</Text>{/if}
    </div>
  </Field>

  <div class="toggle">
    <SwitchRow
      control="end"
      checked={!draft.allowOffline}
      onchange={(v) => (draft.allowOffline = !v)}
      label={t('timers.fieldLiveOnly')}
      hint={t('timers.fieldLiveOnlyHint')}
      hintId="timer-liveonly-desc"
    />
  </div>

  <div class="check">
    <Checkbox bind:checked={draft.enabled}>{t('timers.active')}</Checkbox>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .counter { display: block; text-align: right; margin-top: 4px; }
  .num { display: block; width: 100px; flex: none; }

  .ends-row { display: flex; align-items: center; gap: 10px; }

  .check { margin: 4px 0 6px; --bb-check-align: center; }
  .toggle { margin: 14px 0 10px; }
</style>
