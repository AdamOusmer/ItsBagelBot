<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select } from '@bagel/kit';
  import { namespaceReplyTemplate, namespaceReplySamples, moduleDef } from '@bagel/kit';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, Field, Input, RadioGroup, Text, getI18n, type ChannelPointReward, type CounterScope } from '@bagel/kit';
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

  const DEFAULT_MESSAGE = '{channelpoints:user} redeemed {channelpoints:reward}!';
  const reply = moduleDef('channelpoints')!.replies.find((reply) => reply.key === 'reply')!;
  // svelte-ignore state_referenced_locally
  let message = $state(namespaceReplyTemplate('channelpoints', reply, draft.message));
  $effect(() => { draft.message = message; });
  const payload = $derived(JSON.stringify(draft));

  const samples = $derived<Record<string, string>>(namespaceReplySamples('channelpoints', {
    user: 'sesame_sam',
    input: draft.isUserInputRequired ? 'good luck!' : '',
    reward: draft.title || t('channelpoints.fieldTitle'),
    cost: String(draft.cost || 0),
    channel: 'bagel_bakery',
    counter: '42',
    points: String(draft.points || 0)
  }));

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
    message = namespaceReplyTemplate('channelpoints', reply, message);
    draft.message = message;
    input.formData.set('reward', JSON.stringify(draft));
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
    <Input
      placeholder={t('channelpoints.fieldTitlePh')}
      maxlength="45"
      required
      invalid={!!titleError}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? TITLE_ERR_ID : undefined}
      bind:value={draft.title}
    />
  </Field>

  <div class="field-row">
    <div class="cost-field">
      <Field label={t('channelpoints.fieldCost')}>
        <Input type="number" min="1" bind:value={draft.cost} />
      </Field>
    </div>
    <div class="color-field">
      <Field label={t('channelpoints.fieldColor')}>
        <Input type="color" bind:value={color} />
      </Field>
    </div>
  </div>

  <Field label={t('channelpoints.fieldPrompt')} tag={t('common.optional')}>
    <Input placeholder={t('channelpoints.fieldPromptPh')} maxlength="200" bind:value={draft.prompt} />
  </Field>

  <div class="check">
    <Checkbox bind:checked={draft.isUserInputRequired}>{t('channelpoints.requireInput')}</Checkbox>
  </div>

  <div class="check">
    <Checkbox bind:checked={replyOn}>{t('channelpoints.replyToggle')}</Checkbox>
  </div>

  {#if replyOn}
    <Field label={t('channelpoints.fieldMessage')}>
      <ResponseEditor bind:value={message} surface="reward:channelpoints" placeholder={DEFAULT_MESSAGE} />
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
    <Select
      fill
      bind:value={draft.onRedeem}
      options={[{ value: 'fulfill', label: t('channelpoints.queueFulfill') }, { value: 'cancel', label: t('channelpoints.queueCancel') }, { value: 'leave', label: t('channelpoints.queueLeave') }]}
    />
  </Field>

  <section class="hooks">
    <header class="hooks-head">
      <Text as="span" size="sm" tone="muted">{t('channelpoints.loyaltyTitle')}</Text>
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
            <Input
              placeholder={t('channelpoints.fieldCounterPh')}
              maxlength="64"
              required
              invalid={!!counterError}
              aria-invalid={counterError ? 'true' : undefined}
              aria-describedby={counterError ? COUNTER_ERR_ID : undefined}
              bind:value={draft.counter}
            />
          </Field>

          <Field label={t('rewardCounter.scopeLabel')} hint={scopeDesc}>
            <RadioGroup name="counterScope" bind:value={draft.counterScope} options={scopeOptions} label={t('rewardCounter.scopeLabel')} />
          </Field>

          <div class="token-note"><Text size="xs" mono tone="muted">{t('rewardCounter.tokenNote')}</Text></div>
        </div>
      {/if}
    </div>

    <div class="hook">
      <Checkbox bind:checked={pointsOn}>{t('rewardCounter.pointsEnable')}</Checkbox>
      {#if pointsOn}
        <div class="hook-body">
          <div class="points-field">
            <Field label={t('rewardCounter.pointsLabel')} hint={t('rewardCounter.pointsHint')}>
              <span class="points-input">
                <span class="plus">+</span>
                <span class="points-num"><Input type="number" min="1" bind:value={draft.points} /></span>
              </span>
            </Field>
          </div>
        </div>
      {/if}
    </div>

    {#if counterOn || pointsOn}
      <div class="hook live-gate">
        <Checkbox bind:checked={draft.liveOnly}>{t('rewardCounter.liveOnly')}</Checkbox>
        <span class="live-hint"><Text as="small" size="xs" tone="muted">{t('rewardCounter.liveOnlyHint')}</Text></span>
      </div>
    {/if}
  </section>

  <div class="limits">
    <Text as="span" size="sm" tone="muted">{t('channelpoints.limits')}</Text>

    <div class="limit">
      <Checkbox bind:checked={draft.maxPerStreamEnabled}>{t('channelpoints.limitPerStream')}</Checkbox>
      {#if draft.maxPerStreamEnabled}
        <span class="limit-num"><Input fill type="number" min="1" bind:value={draft.maxPerStream} /></span>
      {/if}
    </div>
    {#if draft.maxPerStreamEnabled}
      <span class="limit-hint"><Text as="small" size="xs" tone="muted">{t('channelpoints.limitPerStreamHint')}</Text></span>
    {/if}

    <div class="limit">
      <Checkbox bind:checked={draft.maxPerUserPerStreamEnabled}>{t('channelpoints.limitPerUser')}</Checkbox>
      {#if draft.maxPerUserPerStreamEnabled}
        <span class="limit-num"><Input fill type="number" min="1" bind:value={draft.maxPerUserPerStream} /></span>
      {/if}
    </div>

    <div class="limit">
      <Checkbox bind:checked={draft.globalCooldownEnabled}>{t('channelpoints.limitCooldown')}</Checkbox>
      {#if draft.globalCooldownEnabled}
        <span class="limit-num"><Input fill type="number" min="1" bind:value={draft.globalCooldownSeconds} /></span>
      {/if}
    </div>
  </div>

  <div class="check">
    <Checkbox bind:checked={draft.isEnabled}>{t('channelpoints.visible')}</Checkbox>
  </div>

  <div class="actions">
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
    <Button variant="primary" type="submit" disabled={busy}>
      {busy ? t('channelpoints.saving') : isNew ? t('channelpoints.create') : t('channelpoints.saveChanges')}
    </Button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }

  .field-row { display: flex; gap: 12px; }
  .cost-field { flex: 1; min-width: 0; }
  .color-field { flex: none; width: 110px; }

  .check { margin: 4px 0 14px; --bb-check-align: center; }

  .hooks {
    display: flex;
    flex-direction: column;
    gap: 6px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    padding: 14px;
    margin-bottom: 14px;
  }
  .hooks-head {
    display: flex;
    align-items: center;
    gap: 7px;
    margin-bottom: 4px;
  }
  .hook { display: flex; flex-direction: column; --bb-check-align: center; }
  .hook + .hook { border-top: 1px solid rgba(var(--bb-white-rgb), 0.06); padding-top: 12px; margin-top: 6px; }

  .hook-body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    margin: 12px 0 4px;
    padding-left: 14px;
    border-left: 2px solid var(--bb-accent-soft);
  }
  .hook-body { --field-mb: 0; }

  .token-note {
    background: rgba(var(--bb-shadow-rgb), 0.28);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    padding: 8px 10px;
  }

  .live-gate { gap: 4px; }
  .live-hint { margin: 2px 0 0 26px; }

  .points-field { max-width: 220px; }
  .points-input { display: flex; align-items: center; gap: 8px; }
  .plus {
    font-family: var(--bb-font-display);
    font-size: var(--bb-text-md);
    color: var(--bb-muted);
    line-height: 1;
  }
  .points-num { display: block; width: 120px; }

  .limits {
    display: flex;
    flex-direction: column;
    gap: 12px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    padding: 14px;
    margin-bottom: 14px;
  }
  .limit { display: flex; align-items: center; gap: 10px; --bb-check-flex: 1; --bb-check-align: center; }
  .limit-num { display: block; width: 88px; flex: none; }
  .limit-hint { margin: -6px 0 0 26px; }

  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 6px; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
    .color-field { width: 100%; }
    .actions { flex-direction: column-reverse; }
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
  }
</style>
