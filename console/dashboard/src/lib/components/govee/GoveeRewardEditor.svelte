<script lang="ts">
  // Inspector body: create/edit the one reward bound to a light. Named form
  // inputs post straight to ?/saveReward (the page owns the enhance handler);
  // local state drives the live ChatPreview rehearsal. The page keys this on the
  // device id so switching lights re-seeds it.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, type GoveeDevice, type GoveeBinding } from '@bagel/shared';
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

  const DEFAULT_REPLY = '@{user} set the lights to {color}!';
  const REPLY_TOKENS = [
    { token: '{user}', label: '{user} → the viewer' },
    { token: '{color}', label: '{color} → what they typed' }
  ];
  const replySamples: Record<string, string> = { user: 'sesame_sam', color: 'blue' };

  // Seeded once per light (the page keys this component on the device id), so
  // capturing the initial binding is intentional.
  // svelte-ignore state_referenced_locally
  const reward = binding?.reward ?? null;
  // svelte-ignore state_referenced_locally
  const isNew = !binding?.rewardId;

  // svelte-ignore state_referenced_locally
  let title = $state(reward?.title || `Colour the ${device.name || 'lights'}`);
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
</script>

<form method="POST" action="?/saveReward" class="editor" use:enhance={onSubmit}>
  <input type="hidden" name="device" value={device.device} />
  <input type="hidden" name="sku" value={device.sku} />
  <input type="hidden" name="deviceName" value={device.name} />

  <p class="hint">
    Viewers type a colour when they redeem: a name (<code>{colors.join(', ')}</code>) or a hex like <code>#00ccff</code>.
  </p>

  <label class="field">
    <span>Reward title</span>
    <input class="input" type="text" name="title" maxlength="45" bind:value={title} required />
  </label>

  <div class="field-row">
    <label class="field">
      <span>Cost (points)</span>
      <input class="input" type="number" name="cost" min="1" max="10000000" bind:value={cost} required />
    </label>
    <label class="field color-field">
      <span>Tile colour</span>
      <input class="color-in" type="color" name="color" bind:value={color} aria-label="Reward tile colour" />
    </label>
  </div>

  <label class="field">
    <span>Cooldown <small>seconds, 0 = none</small></span>
    <input class="input" type="number" name="cooldown" min="0" max="604800" bind:value={cooldown} />
  </label>

  <label class="field">
    <span>Chat reply <small>optional, {'{user}'} and {'{color}'}</small></span>
    <ResponseEditor bind:value={replyMessage} name="replyMessage" tokens={REPLY_TOKENS} placeholder={DEFAULT_REPLY} />
  </label>
  <ChatPreview response={replyMessage || DEFAULT_REPLY} showViewer={false} tag="on redemption" samplesOnly samples={replySamples} />

  <label class="field">
    <span>After it runs</span>
    <select class="input" name="onRedeem" bind:value={onRedeem}>
      <option value="fulfill">Mark fulfilled</option>
      <option value="cancel">Refund the points</option>
      <option value="leave">Leave for a mod</option>
    </select>
  </label>

  <div class="setrow {allowOff ? 'on' : ''}">
    <div class="setrow-text">
      <span class="setrow-label">Let viewers turn the light off</span>
      <span class="muted-text">A viewer can type <code>off</code> to turn this light off.</span>
    </div>
    <button type="button" class="toggle {allowOff ? 'on' : ''}" aria-label="Let viewers turn the light off" onclick={() => (allowOff = !allowOff)}></button>
  </div>
  <input type="hidden" name="allow_off" value={allowOff ? 'on' : ''} />

  <div class="setrow {liveOnly ? '' : 'warn'}">
    <div class="setrow-text">
      <span class="setrow-label">Live only</span>
      <span class="muted-text">
        {liveOnly
          ? 'Redemptions only work while your stream is live (recommended).'
          : 'Off: viewers can change this light even when you are offline.'}
      </span>
    </div>
    <button type="button" class="toggle {liveOnly ? 'on' : ''}" aria-label="Toggle live only" onclick={() => (liveOnly = !liveOnly)}></button>
  </div>
  <input type="hidden" name="allow_offline" value={liveOnly ? '' : 'on'} />

  <div class="actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>Cancel</button>
    <button type="submit" class="btn primary" disabled={busy || !title.trim()}>
      <Icon name="check" size={14} />
      {busy ? 'Saving…' : isNew ? 'Create reward' : 'Save changes'}
    </button>
  </div>

  {#if binding}
    <button type="button" class="btn ghost danger del" onclick={onRequestDelete} disabled={busy}>Delete reward</button>
  {/if}
</form>

<style>
  .editor { padding: 4px 2px 2px; display: grid; gap: 14px; }
  .hint { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }

  .field { display: grid; gap: 5px; }
  .field > span { font-family: var(--bb-font-body); font-size: 12px; font-weight: 600; color: var(--bb-muted); }
  .field > span small { font-weight: 400; opacity: 0.7; }
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

  .field-row { display: flex; gap: 12px; }
  .field-row .field { flex: 1; min-width: 0; }
  .color-field { flex: none; width: 92px; }
  .color-in {
    width: 100%;
    height: 37px;
    padding: 3px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    cursor: pointer;
  }

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
  .setrow .toggle { flex: none; }

  .actions { display: flex; gap: 10px; justify-content: flex-end; }
  .del { justify-self: start; color: #cf8a78; border-color: rgba(176, 90, 70, 0.4); }
  .del:hover { background: rgba(176, 90, 70, 0.1); color: #dc9c8a; }

  code { font-family: var(--bb-font-mono, monospace); font-size: 0.86em; color: var(--bb-tan-light); }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
    .actions { flex-direction: column-reverse; }
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
