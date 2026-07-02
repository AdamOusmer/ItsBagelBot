<script lang="ts">
  import '../app.css';
  import { RootShell } from '@bagel/shared';
  let { children } = $props();
</script>

<RootShell>{@render children()}</RootShell>
