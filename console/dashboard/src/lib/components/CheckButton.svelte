<script lang="ts">
  // Reusable styled checkbox matching the dashboard accent. Two-way bound.
  let { checked = $bindable(false), label = '' }: { checked?: boolean; label?: string } = $props();
</script>

<label class="cb">
  <input type="checkbox" bind:checked />
  <span class="box" aria-hidden="true">
    <svg viewBox="0 0 16 16" width="12" height="12"><path d="M2 8.5l4 4 8-9" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" /></svg>
  </span>
  <span class="lbl">{label}</span>
</label>

<style>
  .cb {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    cursor: pointer;
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.4;
    color: var(--bb-text, #e8e0d6);
    user-select: none;
  }
  .cb input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }
  .box {
    flex: 0 0 auto;
    width: 20px;
    height: 20px;
    margin-top: 1px;
    border-radius: var(--bb-radius-sm, 6px);
    border: 1px solid var(--bb-line, rgba(255, 255, 255, 0.18));
    background: var(--bb-bg-1, #111);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--bb-bg-1, #111);
    transition:
      background var(--bb-dur-fast, 140ms) ease,
      border-color var(--bb-dur-fast, 140ms) ease;
  }
  .box svg {
    opacity: 0;
    transform: scale(0.6);
    transition:
      opacity var(--bb-dur-fast, 140ms) ease,
      transform var(--bb-dur-fast, 140ms) ease;
  }
  .cb input:checked + .box {
    background: var(--bb-tan, #c9a87c);
    border-color: var(--bb-tan, #c9a87c);
  }
  .cb input:checked + .box svg {
    opacity: 1;
    transform: scale(1);
  }
  .cb input:focus-visible + .box {
    outline: 2px solid var(--bb-tan-light, #e0c9a4);
    outline-offset: 2px;
  }
</style>
