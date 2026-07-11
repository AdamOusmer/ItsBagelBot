<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, Scroller, ConfirmDialog, InspectorSurface, MasterToggle, AlertBanner, DeckList, EmptyState, toast, type GoveeDevice } from '@bagel/shared';
  import GoveeLightRow from '$lib/components/govee/GoveeLightRow.svelte';
  import GoveeRewardEditor from '$lib/components/govee/GoveeRewardEditor.svelte';

  let { data } = $props();

  // Local mirrors, reseeded on each SSR load (the /events stream re-runs the
  // loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let keyPresent = $state<boolean>(data.keyPresent ?? false);
  // svelte-ignore state_referenced_locally
  let bindings = $state([...(data.bindings ?? [])]);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      keyPresent = data.keyPresent ?? false;
      bindings = [...(data.bindings ?? [])];
    }
  });

  const bindingFor = (deviceId: string) => bindings.find((b) => b.device === deviceId) ?? null;

  let missingScope = $state(false);

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // formResult is the shared enhance handler for the API-key forms: on success it
  // optionally flips an optimistic mirror, toasts, and reloads.
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

  // --- Inspector -------------------------------------------------------------
  let selected = $state<GoveeDevice | null>(null);
  let busy = $state(false);

  function openLight(d: GoveeDevice) {
    selected = selected?.device === d.device ? null : d;
  }
  function closeInspector() {
    selected = null;
  }

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', 'Reward saved.');
        // Save keeps the inspector open on the bound light; invalidateAll
        // reseeds the binding so it reads current.
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        // Keep the inspector open so the draft survives the reconnect prompt.
        return;
      }
      toast('err', payload?.error ?? 'Could not save the reward.');
    };
  };

  // --- Delete (confirm; Twitch reward deletion is not undoable) ---------------
  let deleteTarget = $state<GoveeDevice | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        if (target && selected?.device === target.device) closeInspector();
        toast('ok', 'Reward deleted.');
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      toast('err', payload?.error ?? 'Could not delete the reward.');
    };
  };

  const colorDevices = (devices: GoveeDevice[]) => devices.filter((d) => d.color);
</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> All modules</a>
  <PageHead eyebrow="Channel points" description="Bind a channel-points reward to each Govee light. Viewers redeem, type a colour (or “off”), and the bot drives that light. One reward per light.">
    Govee <em>Lights</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>Couldn't reach the backend. Try again in a moment.</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner variant="warn" icon="power">
      Reconnect to grant channel-points access.
      {#snippet action()}
        <a class="btn primary" href="/login?next=/govee" data-sveltekit-reload>Reconnect</a>
      {/snippet}
    </AlertBanner>
  {/if}

  <!-- Master switch -->
  <div class="toolbar">
    <MasterToggle
      action="?/toggle"
      bind:enabled
      label="Enable Govee lights"
      hint={enabled ? 'Redemptions drive your lights' : 'Turned off, redemptions are ignored'}
      ariaLabel="Toggle Govee lights"
      failMessage="Could not toggle Govee lights."
    />
  </div>

  <!-- API key -->
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

  {#if keyPresent}
    {#if !enabled}
      <!-- Off but configurable: state it plainly rather than dimming the deck. -->
      <AlertBanner>This module is off. You can set up your lights now; redemptions take effect once you enable it.</AlertBanner>
    {/if}
    <!-- The deck: lights left, docked reward inspector right (same layout as the
         channel-points + commands decks). -->
    <div class="deck {selected ? 'inspecting' : ''}">
      <DeckList>
        {#await data.devices}
          <p class="loading"><span class="spinner" aria-hidden="true"></span> Loading your Govee lights…</p>
        {:then dr}
          {@const lights = colorDevices(dr.devices ?? [])}
          {#if dr.error}
            <p class="err-text"><Icon name="ban" size={13} /> {dr.error}</p>
          {:else if lights.length === 0}
            <EmptyState icon="power" title="No colour-capable Govee lights found on this account." />
          {:else}
            <div class="list">
              {#each lights as d (d.device)}
                <GoveeLightRow
                  device={d}
                  binding={bindingFor(d.device)}
                  expanded={selected?.device === d.device}
                  onExpand={() => openLight(d)}
                  onDelete={() => (deleteTarget = d)}
                />
              {/each}
            </div>
          {/if}
        {/await}
      </DeckList>

      {#if selected}
        <InspectorSurface
          open
          title={selected.name || 'Light'}
          controls="govee-editor"
          closeLabel="Close"
          onClose={closeInspector}
        >
          <Scroller fill padding="16px" data-lenis-prevent>
            {#key selected.device}
              <GoveeRewardEditor
                device={selected}
                binding={bindingFor(selected.device)}
                colors={data.colors}
                {busy}
                onSubmit={saveSubmit}
                onCancel={closeInspector}
                onRequestDelete={() => (deleteTarget = selected)}
              />
            {/key}
          </Scroller>
        </InspectorSurface>
      {/if}
    </div>
  {/if}
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title="Delete this reward?"
  body="This removes the Twitch reward for {deleteTarget?.name || 'this light'} and unbinds the light. Deleting a Twitch reward cannot be undone."
  confirmLabel="Delete"
  cancelLabel="Keep it"
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/deleteReward" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="device" value={deleteTarget?.device ?? ''} />
</form>

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

  .step { display: flex; gap: 14px; align-items: flex-start; }
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
  .step-body h2 { margin: 0 0 6px; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .muted-text { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; margin: 0 0 14px; }
  .muted-text strong { color: var(--bb-tan-light); font-weight: 600; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    min-width: 13rem;
  }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }
  .input::placeholder { color: var(--bb-muted); opacity: 0.7; }

  .btn.danger { color: #cf8a78; }
  .ok-pill { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-green-glow); font-family: var(--bb-font-body); font-size: 13px; font-weight: 600; }

  /* Deck: full-width light list until a selection opens the docked inspector. */
  .deck { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; margin-top: 16px; }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 440px; }
  }
  .list :global(.row-shell:last-child) { border-bottom: none; }

  .loading, .err-text { display: flex; align-items: center; gap: 10px; padding: 16px 16px; margin: 0; font-family: var(--bb-font-body); font-size: 13px; }
  .loading { color: var(--bb-muted); }
  .err-text { color: #cf8a78; gap: 6px; }
  .spinner {
    width: 14px; height: 14px; border-radius: 50%;
    border: 2px solid var(--rule-strong); border-top-color: var(--bb-tan-light);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
