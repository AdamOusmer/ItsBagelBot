<script lang="ts">
  let { message = undefined as string | undefined } = $props();
</script>

{#if message}
  <small class="field-error" role="alert">{message}</small>
{/if}

<style>
  .field-error {
    display: block;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: #cf8a78;
    margin-top: 4px;
  }
</style>
