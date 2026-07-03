<script lang="ts">
  import '../app.css';
  import { RootShell } from '@bagel/shared';
  let { data, children } = $props();
</script>

<RootShell locale={data.locale}>{@render children()}</RootShell>
