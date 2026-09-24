<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Field, RadioGroup, getI18n, type ChannelPointReward, type CounterScope } from '@bagel/kit';
  import { Checkbox } from '@bagel/kit';
  import { focusFirstInvalid } from '@bagel/kit';
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

  let replyOn = $state(draft.action === 'chat');
  $effect(() => {
    draft.action = replyOn ? 'chat' : 'none';
  });

  let color = $state(draft.backgroundColor || '#9147ff');
  $effect(() => {
    draft.backgroundColor = color;
  });

  const DEFAULT_MESSAGE = '{user} redeemed {reward}!';
  const payload = $derived(JSON.stringify(draft));

  const samples = $derived<Record<string, string>>({
    user: 'sesame_sam',
    input: draft.isUserInputRequired ? 'good luck!' : '',
    reward: draft.title || t('channelpoints.fieldTitle'),
    cost: String(draft.cost || 0),
    channel: 'bagel_bakery',
    counter: '42',
    points: String(draft.points || 0)
  });

  let counterOn = $state(!!draft.counter.trim());
  let pointsOn = $state(draft.points > 0);
  $effect(() => {
    if (!counterOn && draft.counter) draft.counter = '';
  });
  $effect(() => {
    if (!pointsOn && draft.points) draft.points = 0;
  });

  const SCOPES: readonly { value: CounterScope; label: string; desc: string }[] = [
    { value: 'viewer_command', label: t('rewardCounter.scopeViewerReward'), desc: t('rewardCounter.scopeViewerRewardDesc') },
    { value: 'command', label: t('rewardCounter.scopeReward'), desc: t('rewardCounter.scopeRewardDesc') },
    { value: 'viewer', label: t('rewardCounter.scopeViewer'), desc: t('rewardCounter.scopeViewerDesc') },
    { value: 'channel', label: t('rewardCounter.scopeChannel'), desc: t('rewardCounter.scopeChannelDesc') }
  ];
  const scopeOptions = SCOPES.map((s) => ({ value: s.value, label: s.label }));
  const scopeDesc = $derived(SCOPES.find((s) => s.value === draft.counterScope)?.desc ?? '');

  const TITLE_ERR_ID = 'reward-title-err';
  const COUNTER_ERR_ID = 'reward-counter-err';
  let attempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);
  const titleError = $derived(attempted && !draft.title.trim() ? t('channelpoints.errTitleRequired') : undefined);
  const counterError = $derived(
    attempted && counterOn && !draft.counter.trim() ? t('rewardCounter.errNameRequired') : undefined
  );

  const submit: SubmitFunction = (input) => {
    attempted = true;
    if (!draft.title.trim() || (counterOn && !draft.counter.trim())) {
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    return onSubmit(input);
  };
</script>

<form
  method="POST"
  action={isNew ? '?/create' : '?/update'}
  class="editor"
  novalidate
  use:enhance={submit}
  bind:this={formEl}
>
  <input type="hidden" name="reward" value={payload} />
  <input type="hidden" name="counter_enabled" value={counterOn ? 'true' : 'false'} />

  <Field label={t('channelpoints.fieldTitle')} error={titleError} errorId={TITLE_ERR_ID}>
    <input
      class="bb-input"
      placeholder={t('channelpoints.fieldTitlePh')}
      maxlength="45"
      required
      data-invalid={titleError ? '' : undefined}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? TITLE_ERR_ID : undefined}
      bind:value={draft.title}
    />
  </Field>

  <div class="field-row">
    <Field label={t('channelpoints.fieldCost')} class="cost-field">
      <input class="bb-input" type="number" min="1" bind:value={draft.cost} />
    </Field>
    <Field label={t('channelpoints.fieldColor')} class="color-field">
      <input class="color-in" type="color" bind:value={color} />
    </Field>
  </div>

  <Field label={t('channelpoints.fieldPrompt')} tag={t('common.optional')}>
    <input class="bb-input" placeholder={t('channelpoints.fieldPromptPh')} maxlength="200" bind:value={draft.prompt} />
  </Field>

  <div class="check">
    <Checkbox bind:checked={draft.isUserInputRequired}>{t('channelpoints.requireInput')}</Checkbox>
  </div>

  <div class="check">
    <Checkbox bind:checked={replyOn}>{t('channelpoints.replyToggle')}</Checkbox>
  </div>

  {#if replyOn}
    <Field label={t('channelpoints.fieldMessage')}>
      <ResponseEditor bind:value={draft.message} surface="reward:channelpoints" placeholder={DEFAULT_MESSAGE} />
    </Field>
    <ChatPreview
      kind="reply"
      response={draft.message || DEFAULT_MESSAGE}
      showViewer={false}
      tag={t('channelpoints.previewTag')}
      {samples}
    />
  {/if}

  <Field label={t('channelpoints.queueTitle')} hint={t('channelpoints.queueHint')}>
    <select class="bb-input" bind:value={draft.onRedeem}>
      <option value="fulfill">{t('channelpoints.queueFulfill')}</option>
      <option value="cancel">{t('channelpoints.queueCancel')}</option>
      <option value="leave">{t('channelpoints.queueLeave')}</option>
    </select>
  </Field>

  <section class="hooks">
    <header class="hooks-head">
      <span>{t('channelpoints.loyaltyTitle')}</span>
    </header>

    <div class="hook">
      <Checkbox bind:checked={counterOn}>{t('rewardCounter.enable')}</Checkbox>
      {#if counterOn}
        <div class="hook-body">
          <Field
            label={t('rewardCounter.nameLabel')}
            hint={t('rewardCounter.nameHint')}
            error={counterError}
            errorId={COUNTER_ERR_ID}
          >
            <input
              class="bb-input"
              placeholder={t('channelpoints.fieldCounterPh')}
              maxlength="64"
              required
              data-invalid={counterError ? '' : undefined}
              aria-invalid={counterError ? 'true' : undefined}
              aria-describedby={counterError ? COUNTER_ERR_ID : undefined}
              bind:value={draft.counter}
            />
          </Field>

          <Field label={t('rewardCounter.scopeLabel')} hint={scopeDesc}>
            <RadioGroup name="counterScope" bind:value={draft.counterScope} options={scopeOptions} label={t('rewardCounter.scopeLabel')} />
          </Field>

          <p class="token-note">{t('rewardCounter.tokenNote')}</p>
        </div>
      {/if}
    </div>

    <div class="hook">
      <Checkbox bind:checked={pointsOn}>{t('rewardCounter.pointsEnable')}</Checkbox>
      {#if pointsOn}
        <div class="hook-body">
          <Field label={t('rewardCounter.pointsLabel')} hint={t('rewardCounter.pointsHint')} class="points-field">
            <div class="points-input">
              <span class="plus">+</span>
              <input class="bb-input num" type="number" min="1" bind:value={draft.points} />
            </div>
          </Field>
        </div>
      {/if}
    </div>

    {#if counterOn || pointsOn}
      <div class="hook live-gate">
        <Checkbox bind:checked={draft.liveOnly}>{t('rewardCounter.liveOnly')}</Checkbox>
        <small class="live-hint">{t('rewardCounter.liveOnlyHint')}</small>
      </div>
    {/if}
  </section>

  <div class="limits">
    <span class="limits-title">{t('channelpoints.limits')}</span>

    <div class="limit">
      <Checkbox bind:checked={draft.maxPerStreamEnabled}>{t('channelpoints.limitPerStream')}</Checkbox>
      {#if draft.maxPerStreamEnabled}
        <input class="bb-input num" type="number" min="1" bind:value={draft.maxPerStream} />
      {/if}
    </div>
    {#if draft.maxPerStreamEnabled}
      <small class="limit-hint">{t('channelpoints.limitPerStreamHint')}</small>
    {/if}

    <div class="limit">
      <Checkbox bind:checked={draft.maxPerUserPerStreamEnabled}>{t('channelpoints.limitPerUser')}</Checkbox>
      {#if draft.maxPerUserPerStreamEnabled}
        <input class="bb-input num" type="number" min="1" bind:value={draft.maxPerUserPerStream} />
      {/if}
    </div>

    <div class="limit">
      <Checkbox bind:checked={draft.globalCooldownEnabled}>{t('channelpoints.limitCooldown')}</Checkbox>
      {#if draft.globalCooldownEnabled}
        <input class="bb-input num" type="number" min="1" bind:value={draft.globalCooldownSeconds} />
      {/if}
    </div>
  </div>

  <div class="check">
    <Checkbox bind:checked={draft.isEnabled}>{t('channelpoints.visible')}</Checkbox>
  </div>

  <div class="actions">
    <button type="button" class="bb-btn bb-btn--ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="submit" class="bb-btn bb-btn--primary" disabled={busy}>
      {busy ? t('channelpoints.saving') : isNew ? t('channelpoints.create') : t('channelpoints.saveChanges')}
    </button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }

  .field-row { display: flex; gap: 12px; }
  :global(.cost-field) { flex: 1; min-width: 0; }
  :global(.color-field) { flex: none; width: 110px; }
  .color-in {
    width: 100%;
    height: 39px;
    padding: 3px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: rgba(0, 0, 0, 0.35);
    cursor: pointer;
  }

  .check { margin: 4px 0 14px; --bb-check-align: center; }

  .hooks {
    display: flex;
    flex-direction: column;
    gap: 6px;
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    border-radius: var(--bb-radius-md);
    padding: 14px;
    margin-bottom: 14px;
  }
  .hooks-head {
    display: flex;
    align-items: center;
    gap: 7px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    letter-spacing: 0.02em;
    color: var(--bb-muted);
    margin-bottom: 4px;
  }
  .hook { display: flex; flex-direction: column; --bb-check-align: center; }
  .hook + .hook { border-top: 1px solid rgba(240, 236, 228, 0.06); padding-top: 12px; margin-top: 6px; }

  .hook-body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    margin: 12px 0 4px;
    padding-left: 14px;
    border-left: 2px solid var(--ui-accent-soft, rgba(240, 236, 228, 0.1));
  }
  .hook-body { --field-mb: 0; }

  .token-note {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    background: rgba(0, 0, 0, 0.28);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    padding: 8px 10px;
    line-height: 1.5;
  }

  .live-gate { gap: 4px; }
  .live-hint {
    color: var(--bb-muted);
    opacity: 0.75;
    font-size: 11px;
    font-family: var(--bb-font-body);
    margin: 2px 0 0 26px;
  }

  :global(.points-field) { max-width: 220px; }
  .points-input { display: flex; align-items: center; gap: 8px; }
  .points-input .plus {
    font-family: var(--bb-font-display);
    font-size: 17px;
    color: var(--bb-muted);
    line-height: 1;
  }
  .points-input .num { width: 120px; }

  .limits {
    display: flex;
    flex-direction: column;
    gap: 12px;
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    border-radius: var(--bb-radius-md);
    padding: 14px;
    margin-bottom: 14px;
  }
  .limits-title {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .limit { display: flex; align-items: center; gap: 10px; --bb-check-flex: 1; --bb-check-align: center; }
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
    :global(.color-field) { width: 100%; }
    .actions { flex-direction: column-reverse; }
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
  }
</style>
