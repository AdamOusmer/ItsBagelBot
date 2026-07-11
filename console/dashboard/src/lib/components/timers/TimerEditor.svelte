<script lang="ts">
  // Timer editor fields (create + edit share it), rendered in the page's docked
  // inspector. Fields only: the page owns the <form> and the sticky EditorFooter
  // so Save/Cancel stay visible below a long form and the save is stale-safe.
  import { getI18n, type TimerDef } from '@bagel/shared';
  import CheckButton from '$lib/components/CheckButton.svelte';

  let {
    draft = $bindable<TimerDef>()
  }: {
    draft: TimerDef;
  } = $props();

  const { t } = getI18n();

  // The wire value is whole seconds; the field reads/writes whole minutes so
  // "every 10" reads naturally instead of "every 600".
  let minutes = $state(Math.max(1, Math.round(draft.intervalSeconds / 60)));
  $effect(() => {
    draft.intervalSeconds = minutes * 60;
  });
</script>

<div class="editor">
  <label class="field">
    <span>{t('timers.fieldMessage')}</span>
    <textarea
      class="search msg-area"
      placeholder={t('timers.fieldMessagePh')}
      maxlength="500"
      required
      rows="3"
      bind:value={draft.message}
    ></textarea>
  </label>

  <label class="field">
    <span>{t('timers.fieldInterval')}</span>
    <div class="interval-row">
      <input class="search num" type="number" min="1" max="1440" bind:value={minutes} />
      <span class="unit">{t('timers.unitMinutes')}</span>
    </div>
    <small>{t('timers.fieldIntervalHint')}</small>
  </label>

  <div class="check">
    <CheckButton bind:checked={draft.enabled} label={t('timers.active')} />
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .field :global(.search) { width: 100%; box-sizing: border-box; }

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
  .interval-row .num { width: 100px; flex: none; }
  .unit { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  .check { margin: 4px 0 14px; }
  .check :global(.cb) { align-items: center; }
</style>
