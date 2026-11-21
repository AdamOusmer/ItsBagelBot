<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import '@bagel/ui/styles/elements/collapse.css';
  import { namespaceReplySamples, getI18n, type ChannelPointReward, type CounterScope } from '@bagel/kit';
  import ResponseEditor from '#lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '#lib/components/commands/ChatPreview.svelte';
  import HeldText from '#lib/components/shared/HeldText.svelte';
  import { REWARD_ERROR_KEYS, rewardErrors, type RewardErrorField } from './reward-draft';

  let {
    draft = $bindable<ChannelPointReward>(),
    counterOn = $bindable(false),
    pointsOn = $bindable(false),
    attempted = false
  }: {
    draft: ChannelPointReward;
    counterOn?: boolean;
    pointsOn?: boolean;
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

  const replyOn = $derived(draft.action === 'chat');
  const gateOn = $derived(counterOn || pointsOn);

  const errors = $derived(rewardErrors(draft, counterOn, pointsOn));
  const shown = (field: RewardErrorField) => (attempted && errors[field] ? t(REWARD_ERROR_KEYS[field]) : undefined);
  const titleError = $derived(shown('title'));
  const costError = $derived(shown('cost'));
  const counterError = $derived(shown('counter'));
  const pointsError = $derived(shown('points'));
  const perStreamError = $derived(shown('perStream'));
  const perStreamNoteOn = $derived(draft.maxPerStreamEnabled || !!perStreamError);
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
        <ColorPicker bind:value={draft.backgroundColor} />
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

  <div class="bb-collapse" class:is-open={replyOn} aria-hidden={!replyOn} inert={!replyOn}>
    <div>
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
    </div>
  </div>

  <Field label={t('channelpoints.queueTitle')} hint={t('channelpoints.queueHint')}>
    <Select
      fill
      bind:value={draft.onRedeem}
      options={[{ value: 'fulfill', label: t('channelpoints.queueFulfill') }, { value: 'cancel', label: t('channelpoints.queueCancel') }, { value: 'leave', label: t('channelpoints.queueLeave') }]}
    />
  </Field>

  <Card style="--card-pad: 16px; margin-bottom: 16px">
  <section class="hooks">
    <header class="hooks-head">
      <Text as="span" size="sm" tone="muted">{t('channelpoints.loyaltyTitle')}</Text>
    </header>

    <div class="hook">
      <Checkbox bind:checked={counterOn}>{t('rewardCounter.enable')}</Checkbox>
      <div class="bb-collapse" class:is-open={counterOn} aria-hidden={!counterOn} inert={!counterOn}>
        <div>
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
                required={counterOn}
                invalid={!!counterError}
                aria-invalid={counterError ? 'true' : undefined}
                aria-describedby={counterError ? 'reward-counter-err' : undefined}
                bind:value={draft.counter}
              />
            </Field>

            <Field label={t('rewardCounter.scopeLabel')} hint={scopeDesc}>
              <RadioGroup name="counterScope" bind:value={draft.counterScope} options={scopeOptions} label={t('rewardCounter.scopeLabel')} />
            </Field>

            <Code block wrap>{t('rewardCounter.tokenNote')}</Code>
          </div>
        </div>
      </div>
    </div>

    <div class="hook">
      <Checkbox bind:checked={pointsOn}>{t('rewardCounter.pointsEnable')}</Checkbox>
      <div class="bb-collapse" class:is-open={pointsOn} aria-hidden={!pointsOn} inert={!pointsOn}>
        <div>
          <div class="hook-body">
            <div class="points-field">
              <Field label={t('rewardCounter.pointsLabel')} hint={t('rewardCounter.pointsHint')} error={pointsError} errorId="reward-points-err">
                <span class="points-input">
                  <span class="plus">+</span>
                  <span class="points-num"><Input
                      type="number"
                      min={1}
                      invalid={!!pointsError}
                      aria-invalid={pointsError ? 'true' : undefined}
                      aria-describedby={pointsError ? 'reward-points-err' : undefined}
                      bind:value={draft.points}
                    /></span>
                </span>
              </Field>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="bb-collapse gate" class:is-open={gateOn} aria-hidden={!gateOn} inert={!gateOn}>
      <div>
        <div class="hook live-gate">
          <Checkbox bind:checked={draft.liveOnly}>{t('rewardCounter.liveOnly')}</Checkbox>
          <span class="live-hint"><Text as="small" size="xs" tone="muted">{t('rewardCounter.liveOnlyHint')}</Text></span>
        </div>
      </div>
    </div>
  </section>
  </Card>

  <Card style="--card-pad: 16px; margin-bottom: 16px">
  <div class="limits">
    <Text as="span" size="sm" tone="muted">{t('channelpoints.limits')}</Text>

    <div class="limit-group">
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
    <div class="bb-collapse" class:is-open={perStreamNoteOn} aria-hidden={!perStreamNoteOn} inert={!perStreamNoteOn}>
      <div>
        <div class="limit-note" role={perStreamError ? 'alert' : undefined}>
          <Text as="small" size="xs" tone={perStreamError ? 'danger' : 'muted'}>{perStreamError ?? t('channelpoints.limitPerStreamHint')}</Text>
        </div>
      </div>
    </div>
    </div>

    <div class="limit-group">
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
    <div class="bb-collapse" class:is-open={!!perUserError} aria-hidden={!perUserError} inert={!perUserError}>
      <div>
        <div class="limit-note" role={perUserError ? 'alert' : undefined}>
          <Text as="small" size="xs" tone="danger"><HeldText text={perUserError} /></Text>
        </div>
      </div>
    </div>
    </div>

    <div class="limit-group">
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
    <div class="bb-collapse" class:is-open={!!cooldownError} aria-hidden={!cooldownError} inert={!cooldownError}>
      <div>
        <div class="limit-note" role={cooldownError ? 'alert' : undefined}>
          <Text as="small" size="xs" tone="danger"><HeldText text={cooldownError} /></Text>
        </div>
      </div>
    </div>
    </div>
  </div>
  </Card>

  <div class="check">
    <Checkbox bind:checked={draft.isEnabled}>{t('channelpoints.visible')}</Checkbox>
  </div>
</div>

<style>
  .editor { padding: 4px 2px 2px; }

  .field-row { display: flex; gap: 12px; }
  .cost-field { flex: 1; min-width: 0; }
  .color-field { flex: none; }

  .check { margin: 4px 0 16px; --bb-check-align: center; }

  .hooks {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .hooks-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 4px;
  }
  .hook { display: flex; flex-direction: column; --bb-check-align: center; }
  .hook + .hook { padding-top: 12px; margin-top: 8px; }

  .hook-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin: 12px 0 4px;
  }
  .hook-body { --field-mb: 0; }

  .gate { --bb-collapse-gap: 8px; }
  .live-gate { gap: 4px; padding-block-start: 16px; }
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
  }
  .limit { display: flex; align-items: center; gap: 8px; --bb-check-flex: 1; --bb-check-align: center; }
  .limit-num { display: block; width: 88px; flex: none; }
  .limit-num.off { visibility: hidden; opacity: 0; }
  .limit-group { display: flex; flex-direction: column; gap: 8px; --bb-collapse-gap: 8px; }
  .limit-note { padding-inline-start: 26px; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
    .color-field { width: 100%; }
  }
</style>
