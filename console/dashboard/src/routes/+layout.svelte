<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { Cursor, initLenis } from '@bagel/shared';
  let { children } = $props();

  onMount(() => {
    let teardown: (() => void) | undefined;
    initLenis().then((fn) => (teardown = fn));
    return () => teardown?.();
  });
</script>

<Cursor />
{@render children()}
