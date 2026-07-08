<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, toast } from '@bagel/shared';

  let { data } = $props();

  // Local mirrors, reseeded on each SSR load (the /events invalidation stream
  // re-runs the loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let selectedDevice = $state<string>(data.binding?.device ?? '');
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      selectedDevice = data.binding?.device ?? '';
    }
  });

  let missingScope = $state(false);

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // formResult is the shared enhance handler: on success it toasts + reloads;
  // on a missing-scope rejection it shows the reconnect CTA; otherwise it toasts
  // the error.
  function formResult(okMsg: string, failMsg: string): SubmitFunction {
    return () =>
      async ({ result }) => {
        const payload = payloadOf(result);
        if (result.type === 'success' && payload?.ok !== false) {
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

  const colorDevices = $derived((data.devices ?? []).filter((d) => d.color));
  const selectedMeta = $derived(colorDevices.find((d) => d.device === selectedDevice));
  const boundReward = $derived(data.binding?.reward ?? null);
</script>

<section class="screen active">
  <PageHead eyebrow="Channel points" description="Let viewers recolour your Govee lights by redeeming channel points. Live only: off-stream redemptions are refunded.">
    Govee <em>Lights</em>
  </PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert"><Icon name="ban" size={13} /> Couldn't reach the backend. Try again in a moment.</div>
  {/if}

  {#if missingScope}
    <div class="reconnect" role="alert">
      <span class="reconnect-text"><Icon name="lock" size={13} /> Reconnect to grant channel-points access.</span>
      <a class="btn primary" href="/login?next=/govee" data-sveltekit-reload>Reconnect</a>
    </div>
  {/if}

  <div class="toolbar">
    <form method="POST" action="?/toggle" use:enhance={masterSubmit} class="master">
      <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
      <button class="toggle {enabled ? 'on' : ''}" type="submit" aria-label="Toggle Govee lights"></button>
    </form>
    <span class="toolbar-label">{enabled ? 'Redemptions drive your lights' : 'Turned off — redemptions are ignored'}</span>
  </div>

  <!-- Step 1: API key -->
  <Card>
    <div class="step">
      <span class="step-num">1</span>
      <div class="step-body">
        <h2>Govee API key</h2>
        <p class="muted">
          Get a key from the Govee mobile app: <strong>Profile → Settings → Apply for API Key</strong>. It is stored
          encrypted — we never show it back.
        </p>
        {#if data.keyPresent}
          <div class="row">
            <span class="ok-pill"><Icon name="check" size={13} /> Key on file</span>
            <form method="POST" action="?/clearKey" use:enhance={formResult('Key removed.', 'Could not remove the key.')}>
              <button class="btn danger" type="submit">Remove key</button>
            </form>
          </div>
        {:else}
          <form method="POST" action="?/saveKey" use:enhance={formResult('Key saved.', 'Could not save the key.')} class="row">
            <input class="input" type="password" name="key" placeholder="Paste your Govee API key" autocomplete="off" required />
            <button class="btn primary" type="submit">Save key</button>
          </form>
        {/if}
      </div>
    </div>
  </Card>

  <!-- Step 2: device -->
  <Card>
    <div class="step {data.keyPresent ? '' : 'disabled'}">
      <span class="step-num">2</span>
      <div class="step-body">
        <h2>Pick the light</h2>
        {#if !data.keyPresent}
          <p class="muted">Add your API key first to load your devices.</p>
        {:else if data.deviceError}
          <p class="err-text"><Icon name="ban" size={13} /> {data.deviceError}</p>
        {:else if colorDevices.length === 0}
          <p class="muted">No colour-capable Govee devices found on this account.</p>
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
            <!-- The sku + name that pair with the chosen device id, resolved from
                 the selection so the server stores a consistent triple. -->
            <input type="hidden" name="sku" value={selectedMeta?.sku ?? ''} />
            <input type="hidden" name="deviceName" value={selectedMeta?.name ?? ''} />
            <button class="btn primary" type="submit" disabled={!selectedDevice}>Save device</button>
          </form>
        {/if}
      </div>
    </div>
  </Card>

  <!-- Step 3: reward -->
  <Card>
    <div class="step {data.binding?.device ? '' : 'disabled'}">
      <span class="step-num">3</span>
      <div class="step-body">
        <h2>The reward</h2>
        {#if !data.binding?.device}
          <p class="muted">Pick a light first.</p>
        {:else}
          <p class="muted">
            Viewers type a colour when they redeem: a name (<code>{data.colors.join(', ')}</code>) or a hex code like
            <code>#00ccff</code>.
          </p>
          <form method="POST" action="?/saveReward" use:enhance={formResult('Reward saved.', 'Could not save the reward.')} class="reward-form">
            <label class="field">
              <span>Title</span>
              <input class="input" type="text" name="title" maxlength="45" value={boundReward?.title ?? 'Colour my lights'} required />
            </label>
            <label class="field">
              <span>Cost (points)</span>
              <input class="input" type="number" name="cost" min="1" max="10000000" value={boundReward?.cost ?? 500} required />
            </label>
            <label class="field">
              <span>After it runs</span>
              <select class="input" name="onRedeem" value={data.binding?.onRedeem ?? 'fulfill'}>
                <option value="fulfill">Mark fulfilled</option>
                <option value="cancel">Refund the points</option>
                <option value="leave">Leave for a mod</option>
              </select>
            </label>
            <div class="reward-actions">
              <button class="btn primary" type="submit">{boundReward ? 'Update reward' : 'Create reward'}</button>
              {#if boundReward}
                <span class="reward-live"><Icon name="check" size={13} /> Live: {boundReward.title} · {boundReward.cost} pts</span>
              {/if}
            </div>
          </form>
          {#if boundReward}
            <form method="POST" action="?/deleteReward" use:enhance={formResult('Reward deleted.', 'Could not delete the reward.')}>
              <button class="btn danger ghost" type="submit">Delete reward</button>
            </form>
          {/if}
        {/if}
      </div>
    </div>
  </Card>
</section>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin: 0.5rem 0 1rem;
  }
  .toolbar-label {
    font-size: 0.85rem;
    color: var(--text-muted, #8a8f98);
  }
  .master {
    display: inline-flex;
  }
  .toggle {
    width: 42px;
    height: 24px;
    border-radius: 999px;
    border: none;
    background: var(--border, #3a3d44);
    position: relative;
    cursor: pointer;
    transition: background 0.15s ease;
  }
  .toggle::after {
    content: '';
    position: absolute;
    top: 3px;
    left: 3px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s ease;
  }
  .toggle.on {
    background: var(--accent, #1f69ff);
  }
  .toggle.on::after {
    transform: translateX(18px);
  }

  .step {
    display: flex;
    gap: 1rem;
    align-items: flex-start;
  }
  .step.disabled {
    opacity: 0.55;
  }
  .step-num {
    flex: none;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent, #1f69ff);
    color: #fff;
    font-weight: 700;
    font-size: 0.85rem;
  }
  .step-body {
    flex: 1;
    min-width: 0;
  }
  .step-body h2 {
    margin: 0 0 0.35rem;
    font-size: 1.05rem;
  }
  .muted {
    color: var(--text-muted, #8a8f98);
    font-size: 0.88rem;
    margin: 0 0 0.75rem;
  }
  .row {
    display: flex;
    gap: 0.6rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .input {
    padding: 0.5rem 0.7rem;
    border-radius: 8px;
    border: 1px solid var(--border, #3a3d44);
    background: var(--surface, #16181d);
    color: inherit;
    font: inherit;
    min-width: 12rem;
  }
  .btn {
    padding: 0.5rem 0.9rem;
    border-radius: 8px;
    border: 1px solid var(--border, #3a3d44);
    background: var(--surface, #16181d);
    color: inherit;
    cursor: pointer;
    font: inherit;
  }
  .btn.primary {
    background: var(--accent, #1f69ff);
    border-color: transparent;
    color: #fff;
  }
  .btn.danger {
    color: #ff6b6b;
  }
  .btn.ghost {
    background: transparent;
    margin-top: 0.5rem;
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .ok-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    color: #48c78e;
    font-size: 0.88rem;
    font-weight: 600;
  }
  .err-text {
    color: #ff6b6b;
    font-size: 0.88rem;
    display: flex;
    align-items: center;
    gap: 0.3rem;
  }
  .devices {
    list-style: none;
    margin: 0 0 0.9rem;
    padding: 0;
    display: grid;
    gap: 0.4rem;
  }
  .device {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.55rem 0.7rem;
    border: 1px solid var(--border, #3a3d44);
    border-radius: 8px;
    cursor: pointer;
  }
  .device.sel {
    border-color: var(--accent, #1f69ff);
    background: color-mix(in srgb, var(--accent, #1f69ff) 12%, transparent);
  }
  .device-name {
    font-weight: 600;
  }
  .device-sku {
    color: var(--text-muted, #8a8f98);
    font-size: 0.8rem;
    margin-left: auto;
  }
  .reward-form {
    display: grid;
    gap: 0.7rem;
    max-width: 22rem;
  }
  .field {
    display: grid;
    gap: 0.25rem;
    font-size: 0.85rem;
  }
  .reward-actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .reward-live {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    color: #48c78e;
    font-size: 0.82rem;
  }
  code {
    font-size: 0.82em;
  }
  .degraded,
  .reconnect {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.6rem 0.8rem;
    border-radius: 8px;
    margin-bottom: 0.8rem;
    font-size: 0.88rem;
  }
  .reconnect {
    justify-content: space-between;
    flex-wrap: wrap;
  }
</style>
