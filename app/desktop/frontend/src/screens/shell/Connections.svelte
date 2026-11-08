<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { Call } from '@wailsio/runtime';
  import { onMount } from 'svelte';

  type Status = { configured: boolean; state: string; error: string; attempts: number };
  let status = $state<Status>({ configured: false, state: 'Not configured', error: '', attempts: 0 });
  let failure = $state('');
  let busy = $state(false);
  async function refresh(): Promise<void> {
    try { status = await Call.ByName('main.BridgeService.Status') as Status; failure = ''; }
    catch { failure = 'Connection status is unavailable.'; }
  }
  async function retry(): Promise<void> {
    busy = true;
    try { await Call.ByName('main.BridgeService.Retry'); failure = ''; await refresh(); }
    catch { failure = 'Could not retry the connection.'; }
    finally { busy = false; }
  }
  onMount(() => {
    let active = true;
    async function update(): Promise<void> { if (active && !busy) { busy = true; try { await refresh(); } finally { busy = false; } } }
    void update();
    const timer = setInterval(() => { void update(); }, 1000);
    return () => { active = false; clearInterval(timer); };
  });
</script>

<div class="page">
  <PageHead title="Connections" />
  <Text>{status.state}</Text>
  {#if !status.configured}
    <Text>The server connection becomes available after native account pairing is configured.</Text>
  {/if}
  <p role="status">{failure || status.error}</p>
  {#if status.configured && status.error && status.state !== 'Sign in again'}
    <Button disabled={busy} onclick={retry}>Retry</Button>
  {/if}
</div>

<style>
  .page { display: flex; flex-direction: column; align-items: flex-start; gap: 20px; }
</style>
