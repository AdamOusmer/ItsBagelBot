<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, Scroller, ConfirmDialog, MasterToggle, AlertBanner, DeckList, EmptyState, toast, type GoveeDevice } from '@bagel/shared';
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
        closeInspector();
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        closeInspector();
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

  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !selected) return;
    const el = e.target as HTMLElement | null;
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return;
    closeInspector();
  }

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
    <!-- The deck: lights left, docked reward inspector right (same layout as the
         channel-points + commands decks). -->
    <div class="deck {selected ? 'inspecting' : ''}" class:muted={!enabled}>
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

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="inspector-backdrop"
        class:open={!!selected}
        role="presentation"
        onclick={closeInspector}
        onkeydown={(e) => { if (e.key === 'Enter') closeInspector(); }}
      ></div>
      <aside class="inspector" class:open={!!selected} aria-label="Reward editor">
        <div class="inspector-head">
          <span class="inspector-tag">{selected ? (selected.name || 'Light') : 'Reward editor'}</span>
          {#if selected}
            <button class="mini" type="button" aria-label="Close" onclick={closeInspector}><Icon name="x" size={14} /></button>
          {/if}
        </div>
        {#if selected}
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
        {:else}
          <div class="inspector-idle">
            <span class="idle-glyph"><Icon name="power" size={18} /></span>
            <p>Pick a light to bind a channel-points reward to it.</p>
          </div>
        {/if}
      </aside>
    </div>
  {/if}
</section>

<svelte:window onkeydown={onKey} />

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

  /* Deck (list + docked inspector), mirroring the channel-points page. */
  .deck { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; margin-top: 16px; transition: opacity var(--bb-dur-fast, 140ms) ease; }
  .deck.muted { opacity: 0.72; }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 440px; }
    .deck { grid-template-columns: minmax(0, 1fr) 320px; }
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

  .inspector {
    position: sticky;
    top: 62px;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 62px - 108px);
  }
  .inspector-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--rule); }
  .inspector-tag { font-family: var(--bb-font-display); font-weight: 700; font-size: 12px; letter-spacing: 0.02em; color: var(--bb-tan); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .inspector-idle { padding: 34px 20px; text-align: center; color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; display: flex; flex-direction: column; align-items: center; gap: 12px; }
  .idle-glyph { display: inline-flex; align-items: center; justify-content: center; width: 40px; height: 40px; border: 1px solid var(--rule-tan); border-radius: 8px; color: var(--bb-tan-light); }
  .inspector-idle p { margin: 0; max-width: 26ch; line-height: 1.5; }

  .inspector-backdrop { display: none; }
  @media (max-width: 1079px) {
    .inspector { display: none; }
    .inspector.open {
      display: flex;
      position: fixed;
      left: 0; right: 0; bottom: 0;
      top: auto;
      z-index: 220;
      max-height: 88vh;
      border-radius: 8px 8px 0 0;
      background: var(--bb-bg-1, #111);
      animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) both;
    }
    .inspector-backdrop.open { display: block; position: fixed; inset: 0; z-index: 219; background: rgba(0, 0, 0, 0.55); }
    @keyframes sheet-in { from { transform: translateY(100%); } to { transform: translateY(0); } }
  }
</style>
