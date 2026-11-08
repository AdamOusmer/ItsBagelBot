<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { publishScene, resetSourceURL } from '../../../lib/overlay/client';
  import { RESOLUTIONS } from '../../../lib/overlay/resolutions';
  import type { OverlayScene } from '../../../lib/overlay/types';

  let { scene, sourceURL, ready, loadError }: {
    scene: OverlayScene;
    sourceURL: string;
    ready: boolean;
    loadError: string;
  } = $props();

  const uid = $props.id();
  const size = $derived(RESOLUTIONS[scene.resolution]);
  let open = $state(false);
  let anchor = $state<HTMLElement>();
  let busy = $state(false);
  let status = $state('');

  async function publish(): Promise<void> {
    busy = true;
    try {
      await publishScene($state.snapshot(scene));
      status = 'Published to OBS and saved on this computer.';
    } catch { status = 'Could not publish. Check the scene and try again.'; }
    finally { busy = false; }
  }

  async function reset(): Promise<void> {
    busy = true;
    try {
      sourceURL = await resetSourceURL();
      status = 'Old URL revoked. Update the Browser Source URL in OBS.';
    } catch { status = 'Could not reset the source URL. Try again.'; }
    finally { busy = false; }
  }
</script>

<span bind:this={anchor}>
  <Button size="sm" onclick={() => (open = !open)}>OBS</Button>
</span>
<PickerPanel bind:open {anchor} label="OBS Browser Source" width={320} maxHeight={400} placement="below">
  <div class="source">
    <Field label="Browser Source URL" for="obs-url-{uid}">
      <Input id="obs-url-{uid}" readonly fill mono value={sourceURL} onclick={(event) => event.currentTarget.select()} />
    </Field>
    <Text>Keep this URL private. Set OBS to {size.width} × {size.height}. Keep ItsBagelBot open.</Text>
    <Button size="sm" disabled={!ready || busy} onclick={publish}>{busy ? 'Please wait…' : 'Publish to OBS'}</Button>
    <Button size="sm" variant="secondary" disabled={!ready || busy} onclick={reset}>Reset private URL</Button>
    <p role="status">{loadError || status}</p>
  </div>
</PickerPanel>

<style>
  .source { display: grid; gap: 12px; padding: 16px; font-size: 12px; }
  .source p { margin: 0; }
</style>
