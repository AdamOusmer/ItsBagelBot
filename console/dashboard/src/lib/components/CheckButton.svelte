<script lang="ts">
  // Reusable styled checkbox matching the dashboard accent. Two-way bound.
  let {
    checked = $bindable(false),
    label = '',
    name,
    value = 'on',
    disabled = false,
    required = false
  }: {
    checked?: boolean;
    label?: string;
    name?: string;
    value?: string;
    disabled?: boolean;
    required?: boolean;
  } = $props();
</script>

<label class="cb" class:disabled>
  <input type="checkbox" {name} {value} bind:checked {disabled} {required} />
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
    font-size: 13.5px;
    line-height: 1.4;
    color: var(--bb-white, var(--bb-text, #e8e0d6));
    user-select: none;
  }
  .cb.disabled {
    cursor: not-allowed;
    opacity: 0.55;
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
    border-radius: 8px 8px;
    border: 1px solid var(--glass-border, var(--bb-line, rgba(255, 255, 255, 0.18)));
    background:
      linear-gradient(var(--glass-fill-2, rgba(255, 255, 255, 0.03)), var(--glass-fill-2, rgba(255, 255, 255, 0.03))),
      rgba(0, 0, 0, 0.22);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: #0a0a0a;
    box-shadow: var(--glass-rim, inset 0 1px 0 rgba(255,255,255,0.16));
    transition:
      background var(--bb-dur-fast, 140ms) ease,
      border-color var(--bb-dur-fast, 140ms) ease,
      box-shadow var(--bb-dur-fast, 140ms) ease,
      transform var(--bb-dur-fast, 140ms) ease;
  }
  .cb:hover .box {
    border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    background: rgba(201, 168, 124, 0.08);
  }
  .cb.disabled:hover .box {
    border-color: var(--glass-border, var(--bb-line, rgba(255, 255, 255, 0.18)));
    background:
      linear-gradient(var(--glass-fill-2, rgba(255, 255, 255, 0.03)), var(--glass-fill-2, rgba(255, 255, 255, 0.03))),
      rgba(0, 0, 0, 0.22);
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
    border-color: var(--bb-tan-light, #e0c9a4);
    box-shadow: var(--bb-shadow-tan-glow, 0 0 0 1px rgba(201,168,124,0.35), 0 0 24px rgba(201,168,124,0.18));
  }
  .cb input:checked + .box svg {
    opacity: 1;
    transform: scale(1);
  }
  .cb input:focus-visible + .box {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 2px;
  }
  .cb input:active + .box {
    transform: scale(0.94);
  }
</style>
