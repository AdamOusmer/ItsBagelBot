<script lang="ts">
  // Styled radio group backed by real <input type="radio"> elements (unlike
  // SegmentedControl, which is JS-only chip state) so it still posts via
  // native form submission / progressive enhancement.
  let {
    name,
    options,
    value = $bindable(''),
    label = 'Options'
  }: {
    name: string;
    options: readonly { value: string; label: string }[];
    value: string;
    label?: string;
  } = $props();
</script>

<div class="radio-group" role="radiogroup" aria-label={label}>
  {#each options as opt (opt.value)}
    <label class="radio-pill" class:on={value === opt.value}>
      <input
        type="radio"
        {name}
        value={opt.value}
        checked={value === opt.value}
        onchange={() => (value = opt.value)}
      />
      <span class="dot" aria-hidden="true"></span>
      {opt.label}
    </label>
  {/each}
</div>

<style>
  .radio-group { display: flex; gap: 10px; flex-wrap: wrap; }
  .radio-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: 0.04em;
    padding: 9px 14px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid var(--glass-border);
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-muted);
    cursor: pointer;
    user-select: none;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .radio-pill:hover { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .radio-pill.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }

  .radio-pill input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }

  .dot {
    flex: 0 0 auto;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 1px solid var(--glass-border);
    background: rgba(0, 0, 0, 0.22);
    transition: all var(--bb-dur-fast, 140ms) ease;
  }
  .radio-pill.on .dot {
    border-color: var(--bb-tan-light, #e0c9a4);
    background: var(--bb-tan, #c9a87c);
    box-shadow: 0 0 0 3px rgba(201, 168, 124, 0.18);
  }
  .radio-pill input:focus-visible ~ .dot {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 2px;
  }
</style>
