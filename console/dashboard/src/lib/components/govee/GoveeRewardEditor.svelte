<script lang="ts">
  // Inspector body: create/edit the one reward bound to a light. Named form
  // inputs post straight to ?/saveReward (the page owns the enhance handler);
  // local state drives the live ChatPreview rehearsal. The page keys this on the
  // device id so switching lights re-seeds it. Save/Cancel live in the sticky
  // EditorFooter (this form's submit button); Delete is a separate control.
  import { tick } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, Field, EditorFooter, Switch, getI18n, type GoveeDevice, type GoveeBinding } from '@bagel/shared';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let {
    device,
    binding,
    colors,
    busy = false,
    onSubmit,
    onCancel,
    onRequestDelete
  }: {
    device: GoveeDevice;
    binding: GoveeBinding | null;
    colors: string[];
    busy?: boolean;
    onSubmit: SubmitFunction;
    onCancel: () => void;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{user} set the lights to {color}!';
  const REPLY_TOKENS = [
    { token: '{user}', label: t('govee.replyTokUser') },
    { token: '{color}', label: t('govee.replyTokColor') }
  ];
  const replySamples: Record<string, string> = { user: 'sesame_sam', color: 'blue' };

  // Seeded once per light (the page keys this component on the device id), so
  // capturing the initial binding is intentional.
  // svelte-ignore state_referenced_locally
  const reward = binding?.reward ?? null;
  // svelte-ignore state_referenced_locally
  const isNew = !binding?.rewardId;

  // svelte-ignore state_referenced_locally
  let title = $state(reward?.title || t('govee.defaultTitle', { name: device.name || t('govee.theLights') }));
  // svelte-ignore state_referenced_locally
  let cost = $state(reward?.cost ?? 500);
  // svelte-ignore state_referenced_locally
  let color = $state(reward?.color || '#9147ff');
  // svelte-ignore state_referenced_locally
  let cooldown = $state(reward?.cooldown ?? 0);
  // svelte-ignore state_referenced_locally
  let onRedeem = $state<string>(binding?.onRedeem ?? 'fulfill');
  // svelte-ignore state_referenced_locally
  let replyMessage = $state(binding?.replyMessage ?? '');
  // svelte-ignore state_referenced_locally
  let allowOff = $state(binding?.allowOff ?? false);
  // liveOnly is the inverse of the stored allowOffline flag; on by default.
  // svelte-ignore state_referenced_locally
  let liveOnly = $state(!binding?.allowOffline);

  // --- Client-side gate: a blank title is the one thing the server can't
  // recover, so validate it before submit and land the caret on it, with the
  // error associated to the input via aria-describedby. ------------------------
  const TITLE_ERR_ID = 'govee-title-err';
  let titleError = $state<string | undefined>(undefined);
  let formEl = $state<HTMLFormElement | null>(null);

  async function focusFirstInvalid() {
    await tick();
    formEl?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }

  const submit: SubmitFunction = (input) => {
    titleError = title.trim() ? undefined : t('govee.errTitleRequired');
    if (titleError) {
      input.cancel();
      void focusFirstInvalid();
      return;
    }
    return onSubmit(input);
  };
</script>

<form method="POST" action="?/saveReward" class="editor" novalidate use:enhance={submit} bind:this={formEl}>
  <input type="hidden" name="device" value={device.device} />
  <input type="hidden" name="sku" value={device.sku} />
  <input type="hidden" name="deviceName" value={device.name} />

  <p class="hint">
    {t('govee.editorHintNames')} <code>{colors.join(', ')}</code>. {t('govee.editorHintHex')} <code>#00ccff</code>.
  </p>

  <Field label={t('govee.fieldTitle')}>
    <input
      class="input"
      type="text"
      name="title"
      maxlength="45"
      bind:value={title}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? TITLE_ERR_ID : undefined}
      required
    />
    {#if titleError}<small id={TITLE_ERR_ID} class="field-error" role="alert">{titleError}</small>{/if}
  </Field>

  <div class="field-row">
    <Field label={t('govee.fieldCost')}>
      <input class="input" type="number" name="cost" min="1" max="10000000" bind:value={cost} required />
    </Field>
    <!-- Colour: a labelled native picker PLUS a text hex readout, so the chosen
         value is legible without relying on the swatch colour alone. -->
    <label class="color-field">
      <span class="color-label">{t('govee.fieldColor')}</span>
      <span class="color-row">
        <input class="color-in" type="color" name="color" bind:value={color} />
        <span class="color-hex">{color}</span>
      </span>
    </label>
  </div>

  <Field label={t('govee.fieldCooldown')} tag={t('govee.fieldCooldownTag')}>
    <input class="input" type="number" name="cooldown" min="0" max="604800" bind:value={cooldown} />
  </Field>

  <Field label={t('govee.fieldReply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={replyMessage} name="replyMessage" tokens={REPLY_TOKENS} placeholder={DEFAULT_REPLY} />
  </Field>
  <ChatPreview response={replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('govee.previewTag')} samplesOnly samples={replySamples} />

  <Field label={t('govee.afterTitle')}>
    <select class="input" name="onRedeem" bind:value={onRedeem}>
      <option value="fulfill">{t('govee.afterFulfill')}</option>
      <option value="cancel">{t('govee.afterCancel')}</option>
      <option value="leave">{t('govee.afterLeave')}</option>
    </select>
  </Field>

  <div class="setrow {allowOff ? 'on' : ''}">
    <div class="setrow-text">
      <span class="setrow-label">{t('govee.allowOffLabel')}</span>
      <span class="muted-text" id="govee-allowoff-desc">{t('govee.allowOffHint')}</span>
    </div>
    <Switch bind:checked={allowOff} label={t('govee.allowOffLabel')} describedby="govee-allowoff-desc" />
  </div>
  <input type="hidden" name="allow_off" value={allowOff ? 'on' : ''} />

  <div class="setrow {liveOnly ? '' : 'warn'}">
    <div class="setrow-text">
      <span class="setrow-label">{t('govee.liveOnlyLabel')}</span>
      <span class="muted-text" id="govee-liveonly-desc">{liveOnly ? t('govee.liveOnlyOn') : t('govee.liveOnlyOff')}</span>
    </div>
    <Switch bind:checked={liveOnly} label={t('govee.liveOnlyLabel')} describedby="govee-liveonly-desc" />
  </div>
  <input type="hidden" name="allow_offline" value={liveOnly ? '' : 'on'} />

  {#if binding}
    <div class="del-row">
      <Button variant="destructive" icon="trash" onclick={onRequestDelete} disabled={busy}>{t('govee.deleteReward')}</Button>
    </div>
  {/if}

  <EditorFooter
    onCancel={onCancel}
    canSave={!busy}
    status={busy ? 'saving' : 'idle'}
    saveLabel={isNew ? t('govee.create') : t('govee.saveChanges')}
    savingLabel={t('govee.saving')}
    cancelLabel={t('common.cancel')}
  />
</form>

<style>
  .editor { padding: 4px 2px 2px; display: grid; gap: 14px; }
  .hint { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }

  /* Field owns label + wiring; strip its default bottom margin inside the grid. */
  .editor :global(.field) { margin-bottom: 0; }
  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    width: 100%;
    box-sizing: border-box;
    transition: border-color var(--bb-dur-fast, 140ms) ease;
  }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }

  .field-error { display: block; margin-top: 4px; font-family: var(--bb-font-body); font-size: 11.5px; color: #cf8a78; }

  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-row :global(.field) { flex: 1; min-width: 0; }

  .color-field { display: flex; flex-direction: column; gap: 6px; flex: none; width: 116px; }
  .color-label { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .color-row { display: flex; align-items: center; gap: 8px; }
  .color-in {
    width: 44px;
    height: 37px;
    padding: 3px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    cursor: pointer;
    flex: none;
  }
  .color-hex { font-family: var(--bb-font-mono, monospace); font-size: 12px; color: var(--bb-tan-light); text-transform: uppercase; }

  .setrow {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 12px;
    border: 1px solid var(--rule);
    border-radius: 8px;
  }
  .setrow.on { border-color: var(--rule-tan); background: rgba(201, 168, 124, 0.06); }
  .setrow-text { display: grid; gap: 2px; flex: 1; min-width: 0; }
  .setrow-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }
  .setrow.warn .setrow-label { color: #d9a441; }
  .muted-text { margin: 0; font-family: var(--bb-font-body); font-size: 12px; line-height: 1.5; color: var(--bb-muted); }

  .del-row { display: flex; }

  code { font-family: var(--bb-font-mono, monospace); font-size: 0.86em; color: var(--bb-tan-light); }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
