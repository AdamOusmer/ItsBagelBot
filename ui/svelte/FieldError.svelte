<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  let { message = undefined as string | undefined, id = undefined as string | undefined } = $props();
</script>

{#if message}
  <small class="bb-field__error" role="alert" {id}>{message}</small>
{/if}
