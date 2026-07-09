<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { tick } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, ConfirmDialog, toast } from '@bagel/shared';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let { data } = $props();

  // Default chat reply when the template is blank (mirrors sesame's default).
  const DEFAULT_REPLY = '@{user} set the lights to {color}!';
  // Reply template palette + rehearsal samples: only {user} and {color} apply.
  const REPLY_TOKENS = [
    { token: '{user}', label: '{user} → the viewer' },
    { token: '{color}', label: '{color} → what they typed' }
  ];
  const replySamples: Record<string, string> = { user: 'sesame_sam', color: 'blue' };

  // Local mirrors, reseeded on each SSR load (the /events invalidation stream
  // re-runs the loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let keyPresent = $state<boolean>(data.keyPresent ?? false);
  // svelte-ignore state_referenced_locally
  let selectedDevice = $state<string>(data.binding?.device ?? '');
  // Live-only reflects the inverse of the stored allowOffline flag; on by default.
  // svelte-ignore state_referenced_locally
  let liveOnly = $state<boolean>(!(data.binding?.allowOffline));
  // Reward editor draft mirrors (colour, cooldown, reply template, off action).
  // svelte-ignore state_referenced_locally
  let rewardColor = $state<string>(data.binding?.reward?.color || '#9147ff');
  // svelte-ignore state_referenced_locally
  let cooldown = $state<number>(data.binding?.reward?.cooldown ?? 0);
  // svelte-ignore state_referenced_locally
  let replyMessage = $state<string>(data.binding?.replyMessage ?? '');
  // svelte-ignore state_referenced_locally
  let allowOff = $state<boolean>(data.binding?.allowOff ?? false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      keyPresent = data.keyPresent ?? false;
      selectedDevice = data.binding?.device ?? '';
      liveOnly = !(data.binding?.allowOffline);
      rewardColor = data.binding?.reward?.color || '#9147ff';
      cooldown = data.binding?.reward?.cooldown ?? 0;
      replyMessage = data.binding?.replyMessage ?? '';
      allowOff = data.binding?.allowOff ?? false;
    }
  });

  let missingScope = $state(false);

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // formResult is the shared enhance handler: on success it optionally flips an
  // optimistic mirror, toasts, and reloads; on a missing-scope rejection it shows
  // the reconnect CTA; otherwise it toasts the error.
  function formResult(okMsg: string, failMsg: string, onOk?: () => void): SubmitFunction {
    return () =>
      async ({ result }) => {
        const payload = payloadOf(result);
        if (result.type === 'success' && payload?.ok !== false) {
          onOk?.();
          toast('ok', okMsg);
          await invalidateAll();
          return;
        }
        if (payload?.missingScope) {
          missingScope = true;
          return;
        }
        toast('err', payload?.error ?? failMsg);
      };
  }

  const masterSubmit: SubmitFunction = () => {
    const was = enabled;
    enabled = !was;
    return async ({ result }) => {
      if (result.type !== 'success') {
        enabled = was;
        toast('err', 'Could not toggle Govee lights.');
      }
    };
  };

  // --- Live-only gate (default on; turning it OFF needs a warning) -----------
  let confirmOff = $state(false);
  let liveOnlyBusy = $state(false);
  let pendingAllowOffline = $state(false);
  let liveOnlyForm = $state<HTMLFormElement | null>(null);

  // submitLiveOnly posts the desired allowOffline state. tick() flushes the
  // bound hidden input before requestSubmit so the form carries the new value.
  async function submitLiveOnly(allowOffline: boolean) {
    pendingAllowOffline = allowOffline;
    await tick();
    liveOnlyForm?.requestSubmit();
  }

  // Turning live-only OFF (allowOffline true) opens the warning; turning it back
  // ON is safe and saves immediately.
  function onToggleLiveOnly() {
    if (liveOnly) confirmOff = true;
    else submitLiveOnly(false);
  }

  const liveOnlySubmit: SubmitFunction = () => {
    liveOnlyBusy = true;
    return async ({ result }) => {
      liveOnlyBusy = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        liveOnly = !pendingAllowOffline;
        toast('ok', liveOnly ? 'Live only is on.' : 'Live only is off. Offline redemptions allowed.');
        await invalidateAll();
        return;
      }
      toast('err', 'Could not change the live-only setting.');
    };
  };

  const boundReward = $derived(data.binding?.reward ?? null);
</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> All modules</a>
  <PageHead eyebrow="Channel points" description="Let viewers recolour your Govee lights by redeeming channel points. Live only: off-stream redemptions are refunded.">
    Govee <em>Lights</em>
  </PageHead>

  {#if data.degraded}
    <div class="note err" role="alert"><Icon name="ban" size={13} /> Couldn't reach the backend. Try again in a moment.</div>
  {/if}

  {#if missingScope}
    <div class="note reconnect" role="alert">
      <span class="note-text"><Icon name="lock" size={13} /> Reconnect to grant channel-points access.</span>
      <a class="btn primary" href="/login?next=/govee" data-sveltekit-reload>Reconnect</a>
    </div>
  {/if}

  <!-- Master switch: same toggle-row surface as the module detail page. -->
  <Card style="padding:0" class="master-card">
    <div class="toggle-row">
      <div class="tr-text">
        <span class="tr-label">Enable Govee lights</span>
        <span class="tr-help">{enabled ? 'Redemptions drive your lights' : 'Turned off, redemptions are ignored'}</span>
      </div>
      <form method="POST" action="?/toggle" use:enhance={masterSubmit} class="master">
        <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
        <button class="toggle {enabled ? 'on' : ''}" type="submit" aria-label="Toggle Govee lights"></button>
      </form>
    </div>
  </Card>

  <div class="steps" class:muted={!enabled}>
    <!-- Step 1: API key -->
    <Card>
      <div class="step">
        <span class="step-index">1</span>
        <div class="step-body">
          <h2>Govee API key</h2>
          <p class="muted-text">
            Get a key from the Govee mobile app: <strong>Profile → Settings → Apply for API Key</strong>. It is stored
            encrypted. We never show it back.
          </p>
          {#if keyPresent}
            <div class="row">
              <span class="ok-pill"><Icon name="check" size={13} /> Key on file</span>
              <form method="POST" action="?/clearKey" use:enhance={formResult('Key removed.', 'Could not remove the key.', () => (keyPresent = false))}>
                <button class="btn danger" type="submit">Remove key</button>
              </form>
            </div>
          {:else}
            <form method="POST" action="?/saveKey" use:enhance={formResult('Key saved.', 'Could not save the key.', () => (keyPresent = true))} class="row">
              <input class="input" type="password" name="key" placeholder="Paste your Govee API key" autocomplete="off" required />
              <button class="btn primary" type="submit">Save key</button>
            </form>
          {/if}
        </div>
      </div>
    </Card>

    <!-- Step 2: device -->
    <Card>
      <div class="step {keyPresent ? '' : 'disabled'}">
        <span class="step-index">2</span>
        <div class="step-body">
          <h2>Pick the light</h2>
          {#if !keyPresent}
            <p class="muted-text">Add your API key first to load your devices.</p>
          {:else}
            {#await data.devices}
              <p class="loading"><span class="spinner" aria-hidden="true"></span> Loading your Govee devices…</p>
            {:then dr}
              {@const colorDevices = (dr.devices ?? []).filter((d) => d.color)}
              {@const selectedMeta = colorDevices.find((d) => d.device === selectedDevice)}
              {#if dr.error}
                <p class="err-text"><Icon name="ban" size={13} /> {dr.error}</p>
              {:else if colorDevices.length === 0}
                <p class="muted-text">No colour-capable Govee devices found on this account.</p>
              {:else}
                <form method="POST" action="?/pickDevice" use:enhance={formResult('Device saved.', 'Could not save the device.')}>
                  <ul class="devices">
                    {#each colorDevices as d (d.device)}
                      <li>
                        <label class="device {selectedDevice === d.device ? 'sel' : ''}">
                          <input type="radio" name="device" value={d.device} checked={selectedDevice === d.device} onchange={() => (selectedDevice = d.device)} />
                          <span class="device-name">{d.name || d.device}</span>
                          <span class="device-sku">{d.sku}</span>
                        </label>
                      </li>
                    {/each}
                  </ul>
                  <!-- The sku + name that pair with the chosen device id, resolved
                       from the selection so the server stores a consistent triple. -->
                  <input type="hidden" name="sku" value={selectedMeta?.sku ?? ''} />
                  <input type="hidden" name="deviceName" value={selectedMeta?.name ?? ''} />
                  <button class="btn primary" type="submit" disabled={!selectedDevice}>Save device</button>
                </form>
              {/if}
            {/await}
          {/if}
        </div>
      </div>
    </Card>

    <!-- Step 3: reward -->
    <Card>
      <div class="step {data.binding?.device ? '' : 'disabled'}">
        <span class="step-index">3</span>
        <div class="step-body">
          <h2>The reward</h2>
          {#if !data.binding?.device}
            <p class="muted-text">Pick a light first.</p>
          {:else}
            <p class="muted-text">
              Viewers type a colour when they redeem: a name (<code>{data.colors.join(', ')}</code>) or a hex code like
              <code>#00ccff</code>.
            </p>
            <form method="POST" action="?/saveReward" use:enhance={formResult('Reward saved.', 'Could not save the reward.')} class="reward-form">
              <label class="field">
                <span>Title</span>
                <input class="input" type="text" name="title" maxlength="45" value={boundReward?.title ?? 'Colour my lights'} required />
              </label>
              <div class="field-row">
                <label class="field">
                  <span>Cost (points)</span>
                  <input class="input" type="number" name="cost" min="1" max="10000000" value={boundReward?.cost ?? 500} required />
                </label>
                <label class="field color-field">
                  <span>Tile colour</span>
                  <input class="color-in" type="color" name="color" bind:value={rewardColor} aria-label="Reward tile colour" />
                </label>
              </div>
              <label class="field">
                <span>Cooldown <small>seconds between redemptions, 0 = none</small></span>
                <input class="input" type="number" name="cooldown" min="0" max="604800" bind:value={cooldown} />
              </label>

              <label class="field">
                <span>Chat reply <small>optional, {'{user}'} and {'{color}'}</small></span>
                <ResponseEditor bind:value={replyMessage} name="replyMessage" tokens={REPLY_TOKENS} placeholder={DEFAULT_REPLY} />
              </label>
              <ChatPreview response={replyMessage || DEFAULT_REPLY} showViewer={false} tag="on redemption" samplesOnly samples={replySamples} />

              <label class="field">
                <span>After it runs</span>
                <select class="input" name="onRedeem" value={data.binding?.onRedeem ?? 'fulfill'}>
                  <option value="fulfill">Mark fulfilled</option>
                  <option value="cancel">Refund the points</option>
                  <option value="leave">Leave for a mod</option>
                </select>
              </label>

              <!-- Opt-in off action. It is a toggle, not a force: with it on, a
                   viewer typing "off" turns the light off; with it off, "off" is
                   just an unrecognized colour and refunds. -->
              <div class="offrow {allowOff ? 'on' : ''}">
                <div class="offrow-text">
                  <span class="offrow-label">Let viewers turn the lights off</span>
                  <span class="muted-text">A viewer can type <code>off</code> to turn the light off instead of setting a colour.</span>
                </div>
                <button type="button" class="toggle {allowOff ? 'on' : ''}" aria-label="Let viewers turn the lights off" onclick={() => (allowOff = !allowOff)}></button>
              </div>
              <input type="hidden" name="allow_off" value={allowOff ? 'on' : ''} />

              <div class="reward-actions">
                <button class="btn primary" type="submit">{boundReward ? 'Update reward' : 'Create reward'}</button>
                {#if boundReward}
                  <span class="reward-live"><Icon name="check" size={13} /> Live: {boundReward.title} · {boundReward.cost} pts</span>
                {/if}
              </div>
            </form>
            {#if boundReward}
              <form method="POST" action="?/deleteReward" use:enhance={formResult('Reward deleted.', 'Could not delete the reward.')}>
                <button class="btn ghost danger" type="submit">Delete reward</button>
              </form>
            {/if}

            <div class="liveonly {liveOnly ? '' : 'warn'}">
              <div class="liveonly-text">
                <span class="liveonly-label">Live only</span>
                <span class="muted-text">
                  {liveOnly
                    ? 'Redemptions only work while your stream is live (recommended).'
                    : 'Off: viewers can change your lights even when you are offline.'}
                </span>
              </div>
              <button type="button" class="toggle {liveOnly ? 'on' : ''}" aria-label="Toggle live only" onclick={onToggleLiveOnly}></button>
            </div>
          {/if}
        </div>
      </div>
    </Card>
  </div>
</section>

<!-- Hidden form the toggle submits; the value is set before requestSubmit. -->
<form method="POST" action="?/liveOnly" use:enhance={liveOnlySubmit} bind:this={liveOnlyForm} hidden>
  <input type="hidden" name="allow_offline" value={pendingAllowOffline ? 'on' : ''} />
</form>

<ConfirmDialog
  open={confirmOff}
  title="Turn off Live only?"
  body="With Live only off, anyone who redeems this reward can change your Govee lights even while you are offline or away from your setup. Only turn this off to test. You can turn it back on any time."
  confirmLabel="Turn it off"
  cancelLabel="Keep it on"
  danger
  busy={liveOnlyBusy}
  onCancel={() => (confirmOff = false)}
  onConfirm={() => {
    confirmOff = false;
    submitLiveOnly(true);
  }}
/>

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    text-decoration: none;
    margin-bottom: 10px;
  }
  .back:hover { color: var(--bb-white); }

  /* Inline notes: degraded backend + reconnect CTA. */
  .note {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-radius: 8px;
    margin-bottom: 14px;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }
  .note.err {
    border: 1px solid rgba(176, 90, 70, 0.4);
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
  }
  .note.reconnect {
    justify-content: space-between;
    flex-wrap: wrap;
    border: 1px solid var(--rule-strong);
    background: var(--glass-fill);
    color: var(--bb-white);
  }
  .note-text { display: inline-flex; align-items: center; gap: 8px; }

  /* Master switch surface (mirrors the module detail settings card). */
  :global(.master-card) { margin-bottom: 16px; }
  .toggle-row { display: flex; align-items: center; gap: 12px; padding: 16px 18px; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; }
  .tr-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .tr-help { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .master { display: inline-flex; }

  /* Stepped setup: one card per step, dimmed as a group when the module is off. */
  .steps {
    display: grid;
    gap: 14px;
    transition: opacity var(--bb-dur-fast, 140ms) ease;
  }
  .steps.muted { opacity: 0.72; }

  .step { display: flex; gap: 14px; align-items: flex-start; }
  .step.disabled { opacity: 0.55; }
  .step-index {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono, "DM Mono", monospace);
    font-weight: 600;
    font-size: 14px;
  }
  .step-body { flex: 1; min-width: 0; }
  .step-body h2 {
    margin: 0 0 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
  }
  .muted-text {
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.55;
    margin: 0 0 14px;
  }
  .muted-text strong { color: var(--bb-tan-light); font-weight: 600; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  /* Inputs match the module detail .setting-input token style. */
  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    min-width: 13rem;
    transition: border-color var(--bb-dur-fast, 140ms) ease;
  }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }
  .input::placeholder { color: var(--bb-muted); opacity: 0.7; }

  /* danger buttons layer over the global .btn base. */
  .btn.danger { color: #cf8a78; }
  .btn.ghost.danger { margin-top: 10px; border-color: rgba(176, 90, 70, 0.4); }
  .btn.ghost.danger:hover { background: rgba(176, 90, 70, 0.1); color: #dc9c8a; }

  .ok-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--bb-green-glow);
    font-family: var(--bb-font-body);
    font-size: 13px;
    font-weight: 600;
  }
  .err-text {
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
  }
  .loading {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
    margin: 0;
  }
  .spinner {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 2px solid var(--rule-strong);
    border-top-color: var(--bb-tan-light);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .devices { list-style: none; margin: 0 0 14px; padding: 0; display: grid; gap: 6px; }
  .device {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 12px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    cursor: pointer;
    transition: border-color var(--bb-dur-fast, 140ms) ease, background var(--bb-dur-fast, 140ms) ease;
  }
  .device:hover { border-color: var(--rule-strong); }
  .device.sel {
    border-color: var(--rule-tan);
    background: rgba(201, 168, 124, 0.08);
  }
  .device input { accent-color: var(--bb-tan); }
  .device-name { font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-white); }
  .device-sku { color: var(--bb-muted); font-family: var(--bb-font-mono, monospace); font-size: 11.5px; margin-left: auto; }

  .reward-form { display: grid; gap: 12px; max-width: 30rem; }
  .field { display: grid; gap: 5px; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12px;
    font-weight: 600;
    color: var(--bb-muted);
  }
  .field > span small { font-weight: 400; opacity: 0.7; }

  /* Cost + colour side by side, like the channel-points reward editor. */
  .field-row { display: flex; gap: 12px; }
  .field-row .field { flex: 1; min-width: 0; }
  .color-field { flex: none; width: 96px; }
  .color-in {
    width: 100%;
    height: 38px;
    padding: 3px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    cursor: pointer;
  }

  /* Opt-in off-action row (styled like the live-only row). */
  .offrow {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--rule);
    border-radius: 8px;
  }
  .offrow.on { border-color: var(--rule-tan); background: rgba(201, 168, 124, 0.06); }
  .offrow-text { display: grid; gap: 3px; flex: 1; min-width: 0; }
  .offrow-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }
  .offrow-text .muted-text { margin: 0; }
  .offrow .toggle { flex: none; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }

  .reward-actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .reward-live {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--bb-green-glow);
    font-family: var(--bb-font-body);
    font-size: 12.5px;
  }

  .liveonly {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 16px;
    padding-top: 14px;
    border-top: 1px solid var(--rule);
  }
  .liveonly-text { display: grid; gap: 3px; flex: 1; min-width: 0; }
  .liveonly-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }
  .liveonly-text .muted-text { margin: 0; }
  .liveonly.warn .liveonly-label { color: #d9a441; }

  code {
    font-family: var(--bb-font-mono, monospace);
    font-size: 0.86em;
    color: var(--bb-tan-light);
  }
</style>
