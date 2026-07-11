<script lang="ts">
  // Inline reward editor (create + edit share it), rendered in the page's
  // docked inspector — the same surface as the command editor. The whole draft
  // travels as one JSON field; the server validates and normalizes it.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, getI18n, type ChannelPointReward } from '@bagel/shared';
  import CheckButton from '$lib/components/CheckButton.svelte';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let {
    draft = $bindable<ChannelPointReward>(),
    isNew,
    busy = false,
    onCancel,
    onSubmit
  }: {
    draft: ChannelPointReward;
    isNew: boolean;
    busy?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();

  // The bot-reply switch IS the action kind: on = 'chat', off = 'none'.
  let replyOn = $state(draft.action === 'chat');
  $effect(() => {
    draft.action = replyOn ? 'chat' : 'none';
  });

  // Twitch renders the reward tile in this color; empty means Twitch's default.
  let color = $state(draft.backgroundColor || '#9147ff');
  $effect(() => {
    draft.backgroundColor = color;
  });

  const DEFAULT_MESSAGE = '{user} redeemed {reward}!';
  const payload = $derived(JSON.stringify(draft));

  // Reward reply token palette (replaces the command tokens).
  const TOKENS = [
    { token: '{user}', label: t('channelpoints.tokUser') },
    { token: '{input}', label: t('channelpoints.tokInput') },
    { token: '{reward}', label: t('channelpoints.tokReward') },
    { token: '{cost}', label: t('channelpoints.tokCost') },
    { token: '{counter}', label: t('channelpoints.tokCounter') },
    { token: '{points}', label: t('channelpoints.tokPoints') }
  ];

  // Rehearsal samples: substitute ONLY the reward tokens (samplesOnly) with the
  // draft's own values so the preview shows the real reply, not placeholders.
  const samples = $derived<Record<string, string>>({
    user: 'sesame_sam',
    input: draft.isUserInputRequired ? 'good luck!' : '',
    reward: draft.title || t('channelpoints.fieldTitle'),
    cost: String(draft.cost || 0),
    counter: '42',
    points: String(draft.points || 0)
  });
</script>

<form method="POST" action={isNew ? '?/create' : '?/update'} class="editor" novalidate use:enhance={onSubmit}>
  <input type="hidden" name="reward" value={payload} />

  <label class="field">
    <span>{t('channelpoints.fieldTitle')}</span>
    <input class="search" placeholder={t('channelpoints.fieldTitlePh')} maxlength="45" required bind:value={draft.title} />
  </label>

  <div class="field-row">
    <label class="field">
      <span>{t('channelpoints.fieldCost')}</span>
      <input class="search" type="number" min="1" bind:value={draft.cost} />
    </label>
    <label class="field color-field">
      <span>{t('channelpoints.fieldColor')}</span>
      <input class="color-in" type="color" bind:value={color} />
    </label>
  </div>

  <label class="field">
    <span>{t('channelpoints.fieldPrompt')} <small>{t('common.optional')}</small></span>
    <input class="search" placeholder={t('channelpoints.fieldPromptPh')} maxlength="200" bind:value={draft.prompt} />
  </label>

  <div class="check">
    <CheckButton bind:checked={draft.isUserInputRequired} label={t('channelpoints.requireInput')} />
  </div>

  <div class="check">
    <CheckButton bind:checked={replyOn} label={t('channelpoints.replyToggle')} />
  </div>

  {#if replyOn}
    <label class="field">
      <span>{t('channelpoints.fieldMessage')}</span>
      <ResponseEditor bind:value={draft.message} tokens={TOKENS} placeholder={DEFAULT_MESSAGE} />
    </label>
    <ChatPreview
      response={draft.message || DEFAULT_MESSAGE}
      showViewer={false}
      tag={t('channelpoints.previewTag')}
      samplesOnly
      {samples}
    />
  {/if}

  <label class="field">
    <span>{t('channelpoints.queueTitle')}</span>
    <select class="search" bind:value={draft.onRedeem}>
      <option value="fulfill">{t('channelpoints.queueFulfill')}</option>
      <option value="cancel">{t('channelpoints.queueCancel')}</option>
      <option value="leave">{t('channelpoints.queueLeave')}</option>
    </select>
    <small>{t('channelpoints.queueHint')}</small>
  </label>

  <!-- Loyalty hooks: bump a counter and/or award channel currency per redemption. -->
  <div class="limits">
    <span class="limits-title">{t('channelpoints.loyaltyTitle')}</span>
    <label class="field" style="margin-bottom:0">
      <span>{t('channelpoints.fieldCounter')} <small>{t('common.optional')}</small></span>
      <input class="search" placeholder={t('channelpoints.fieldCounterPh')} maxlength="64" bind:value={draft.counter} />
      <small>{t('channelpoints.fieldCounterHint')}</small>
    </label>
    <label class="field" style="margin-bottom:0">
      <span>{t('channelpoints.fieldPoints')} <small>{t('common.optional')}</small></span>
      <input class="search num" type="number" min="0" bind:value={draft.points} />
      <small>{t('channelpoints.fieldPointsHint')}</small>
    </label>
  </div>

  <div class="limits">
    <span class="limits-title">{t('channelpoints.limits')}</span>

    <div class="limit">
      <CheckButton bind:checked={draft.maxPerStreamEnabled} label={t('channelpoints.limitPerStream')} />
      {#if draft.maxPerStreamEnabled}
        <input class="search num" type="number" min="1" bind:value={draft.maxPerStream} />
      {/if}
    </div>
    {#if draft.maxPerStreamEnabled}
      <small class="limit-hint">{t('channelpoints.limitPerStreamHint')}</small>
    {/if}

    <div class="limit">
      <CheckButton bind:checked={draft.maxPerUserPerStreamEnabled} label={t('channelpoints.limitPerUser')} />
      {#if draft.maxPerUserPerStreamEnabled}
        <input class="search num" type="number" min="1" bind:value={draft.maxPerUserPerStream} />
      {/if}
    </div>

    <div class="limit">
      <CheckButton bind:checked={draft.globalCooldownEnabled} label={t('channelpoints.limitCooldown')} />
      {#if draft.globalCooldownEnabled}
        <input class="search num" type="number" min="1" bind:value={draft.globalCooldownSeconds} />
      {/if}
    </div>
  </div>

  <div class="check">
    <CheckButton bind:checked={draft.isEnabled} label={t('channelpoints.visible')} />
  </div>

  <div class="actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="submit" class="btn primary" disabled={busy || !draft.title.trim()}>
      <Icon name="check" size={14} />
      {busy ? t('channelpoints.saving') : isNew ? t('channelpoints.create') : t('channelpoints.saveChanges')}
    </button>
  </div>
</form>

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

  .field-row { display: flex; gap: 12px; }
  .field-row .field { flex: 1; min-width: 0; }
  .color-field { flex: none; width: 110px; }
  .color-in {
    width: 100%;
    height: 39px;
    padding: 3px;
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.35);
    cursor: pointer;
  }

  .check { margin: 4px 0 14px; }
  .check :global(.cb) { align-items: center; }

  .limits {
    display: flex;
    flex-direction: column;
    gap: 12px;
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    border-radius: 8px;
    padding: 14px;
    margin-bottom: 14px;
  }
  .limits-title {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .limit { display: flex; align-items: center; gap: 10px; }
  .limit :global(.cb) { flex: 1; align-items: center; }
  .limit .num { width: 88px; flex: none; }
  .limit-hint {
    color: var(--bb-muted);
    opacity: 0.7;
    font-size: 11px;
    font-family: var(--bb-font-body);
    margin: -6px 0 0 26px;
  }

  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 6px; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
    .color-field { width: 100%; }
    .actions { flex-direction: column-reverse; }
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
