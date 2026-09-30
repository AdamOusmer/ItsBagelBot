<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select, Field, Input, RadioGroup, Checkbox, Text } from '@bagel/ui/svelte';
  import { namespaceReplySamples, getI18n, type ChannelPointReward, type CounterScope } from '@bagel/kit';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import { rewardErrors, type RewardErrorField } from './reward-draft';

  let {
    draft = $bindable<ChannelPointReward>(),
    counterOn = $bindable(false),
    attempted = false
  }: {
    draft: ChannelPointReward;
    counterOn?: boolean;
    attempted?: boolean;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_MESSAGE = '{channelpoints:user} redeemed {channelpoints:reward}!';

  const samples = $derived<Record<string, string>>(namespaceReplySamples('channelpoints', {
    user: 'sesame_sam',
    input: draft.isUserInputRequired ? 'good luck!' : '',
    reward: draft.title || t('channelpoints.fieldTitle'),
    cost: String(draft.cost || 0),
    channel: 'bagel_bakery',
    counter: '42',
    points: String(draft.points || 0)
  }));

  // svelte-ignore state_referenced_locally
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

  const errors = $derived(rewardErrors(draft, counterOn));
  const shown = (field: RewardErrorField) => (attempted && errors[field] ? t(errors[field]!) : undefined);
  const titleError = $derived(shown('title'));
  const costError = $derived(shown('cost'));
  const counterError = $derived(shown('counter'));
  const perStreamError = $derived(shown('perStream'));
  const perUserError = $derived(shown('perUser'));
  const cooldownError = $derived(shown('cooldown'));
</script>

<div class="editor">
  <Field label={t('channelpoints.fieldTitle')} error={titleError} errorId="reward-title-err">
    <Input
      placeholder={t('channelpoints.fieldTitlePh')}
      maxlength={45}
      required
      invalid={!!titleError}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? 'reward-title-err' : undefined}
      bind:value={draft.title}
    />
  </Field>

  <div class="field-row">
    <div class="cost-field">
      <Field label={t('channelpoints.fieldCost')} error={costError} errorId="reward-cost-err">
        <Input
          type="number"
          min={1}
          invalid={!!costError}
          aria-invalid={costError ? 'true' : undefined}
          aria-describedby={costError ? 'reward-cost-err' : undefined}
          bind:value={draft.cost}
        />
      </Field>
    </div>
    <div class="color-field">
      <Field label={t('channelpoints.fieldColor')}>
        <Input type="color" bind:value={draft.backgroundColor} />
      </Field>
    </div>
  </div>

  <Field label={t('channelpoints.fieldPrompt')} tag={t('common.optional')}>
    <Input placeholder={t('channelpoints.fieldPromptPh')} maxlength={200} bind:value={draft.prompt} />
  </Field>

  <div class="check">
    <Checkbox bind:checked={draft.isUserInputRequired}>{t('channelpoints.requireInput')}</Checkbox>
  </div>

  <div class="check">
    <Checkbox bind:checked={() => draft.action === 'chat', (on: boolean) => (draft.action = on ? 'chat' : 'none')}>
      {t('channelpoints.replyToggle')}
    </Checkbox>
  </div>

  {#if draft.action === 'chat'}
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
            errorId="reward-counter-err"
          >
            <Input
              placeholder={t('channelpoints.fieldCounterPh')}
              maxlength={64}
              required
              invalid={!!counterError}
              aria-invalid={counterError ? 'true' : undefined}
              aria-describedby={counterError ? 'reward-counter-err' : undefined}
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
                <span class="points-num"><Input type="number" min={1} bind:value={draft.points} /></span>
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
      <span class="limit-num" class:off={!draft.maxPerStreamEnabled}>
        <Input
          fill
          type="number"
          min={1}
          aria-label={t('channelpoints.limitPerStream')}
          aria-invalid={perStreamError ? 'true' : undefined}
          invalid={!!perStreamError}
          bind:value={draft.maxPerStream}
        />
      </span>
    </div>
    <span class="limit-note" class:off={!draft.maxPerStreamEnabled && !perStreamError} role={perStreamError ? 'alert' : undefined}>
      <Text as="small" size="xs" tone={perStreamError ? 'danger' : 'muted'}>{perStreamError ?? t('channelpoints.limitPerStreamHint')}</Text>
    </span>

    <div class="limit">
      <Checkbox bind:checked={draft.maxPerUserPerStreamEnabled}>{t('channelpoints.limitPerUser')}</Checkbox>
      <span class="limit-num" class:off={!draft.maxPerUserPerStreamEnabled}>
        <Input
          fill
          type="number"
          min={1}
          aria-label={t('channelpoints.limitPerUser')}
          aria-invalid={perUserError ? 'true' : undefined}
          invalid={!!perUserError}
          bind:value={draft.maxPerUserPerStream}
        />
      </span>
    </div>
    <span class="limit-note" role={perUserError ? 'alert' : undefined}>
      <Text as="small" size="xs" tone="danger">{perUserError ?? ''}</Text>
    </span>

    <div class="limit">
      <Checkbox bind:checked={draft.globalCooldownEnabled}>{t('channelpoints.limitCooldown')}</Checkbox>
      <span class="limit-num" class:off={!draft.globalCooldownEnabled}>
        <Input
          fill
          type="number"
          min={1}
          aria-label={t('channelpoints.limitCooldown')}
          aria-invalid={cooldownError ? 'true' : undefined}
          invalid={!!cooldownError}
          bind:value={draft.globalCooldownSeconds}
        />
      </span>
    </div>
    <span class="limit-note" role={cooldownError ? 'alert' : undefined}>
      <Text as="small" size="xs" tone="danger">{cooldownError ?? ''}</Text>
    </span>
  </div>

  <div class="check">
    <Checkbox bind:checked={draft.isEnabled}>{t('channelpoints.visible')}</Checkbox>
  </div>
</div>

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
  .limit-num.off { visibility: hidden; opacity: 0; }
  .limit-note { display: block; margin: -6px 0 0 26px; min-height: 16px; }
  .limit-note.off { visibility: hidden; opacity: 0; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
    .color-field { width: 100%; }
  }
</style>
