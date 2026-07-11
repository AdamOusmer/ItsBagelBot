<script lang="ts">
  // The one switch. Replaces the copy-pasted `.toggle` button that lived in
  // Toggle, MasterToggle, and ~17 route components, each with its own markup and
  // most with no role/aria-checked and a 38x22 (sub-44px) hit area. This owns the
  // pill visuals, an honest role="switch" + aria-checked, a label, an optional
  // description association, disabled/pending states, a visible focus ring, and a
  // 44x44 hit area under any pointer.
  //
  // Two modes:
  //   - control (default): type="button", bindable `checked`, fires onchange.
  //   - form submit: type="submit" for a server-form master toggle (MasterToggle);
  //     the parent form owns the state flip, so a click submits without a local
  //     pre-toggle.
  let {
    checked = $bindable(false),
    label,
    describedby,
    disabled = false,
    pending = false,
    type = 'button',
    onchange
  }: {
    checked?: boolean;
    // Accessible name. Required — a switch with no label is unusable by AT.
    label: string;
    // id of visible descriptive text, wired to aria-describedby.
    describedby?: string;
    disabled?: boolean;
    // In flight: non-interactive but still reflects its current checked state.
    pending?: boolean;
    type?: 'button' | 'submit';
    onchange?: (v: boolean) => void;
  } = $props();

  function flip() {
    if (disabled || pending) return;
    if (type === 'submit') return; // the form owns the state change on submit
    checked = !checked;
    onchange?.(checked);
  }
</script>

<button
  {type}
  class="switch"
  class:on={checked}
  class:pending
  role="switch"
  aria-checked={checked}
  aria-label={label}
  aria-describedby={describedby}
  aria-busy={pending}
  disabled={disabled || pending}
  onclick={flip}
></button>

<style>
  .switch {
    width: 38px; height: 22px; flex-shrink: 0;
    display: inline-block; position: relative;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid var(--glass-border);
    cursor: pointer;
    transition: background var(--bb-dur-base) var(--bb-ease-out-back),
      border-color var(--bb-dur-base) var(--bb-ease-out-back);
  }
  /* 44x44 hit area without growing the visual pill (WCAG 2.2 target size). */
  .switch::before {
    content: '';
    position: absolute;
    inset: -11px -3px;
  }
  /* the knob */
  .switch::after {
    content: '';
    position: absolute; top: 2px; left: 2px;
    width: 16px; height: 16px; border-radius: 50%;
    background: var(--bb-muted);
    transition: left var(--bb-dur-base) var(--bb-ease-out-back),
      background var(--bb-dur-base) var(--bb-ease-out-back),
      box-shadow var(--bb-dur-base) var(--bb-ease-out-back);
  }
  .switch.on {
    background: var(--ui-accent-soft);
    border-color: rgba(82, 183, 136, 0.4);
  }
  .switch.on::after {
    left: 18px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px var(--bb-green-glow);
  }

  .switch:focus-visible {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 3px;
  }

  .switch:disabled { cursor: default; opacity: 0.55; }
  .switch.pending { cursor: progress; }

  @media (prefers-reduced-motion: reduce) {
    .switch, .switch::after { transition: none; }
  }
</style>
