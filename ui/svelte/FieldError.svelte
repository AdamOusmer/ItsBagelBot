<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Standalone validation message, for call sites that render the control
  // themselves instead of wrapping it in <Field>. Same class and same
  // role="alert" as Field's own error line, so the two cannot drift apart.
  let { message = undefined as string | undefined, id = undefined as string | undefined } = $props();
</script>

{#if message}
  <small class="bb-field__error" role="alert" {id}>{message}</small>
{/if}
