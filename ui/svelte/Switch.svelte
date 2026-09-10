<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // The one switch. Replaces the copy-pasted `.toggle` button that lived in
  // Toggle, MasterToggle and ~17 route components, each with its own markup and
  // most with no role/aria-checked. This owns an honest role="switch" +
  // aria-checked, a mandatory label, an optional description association,
  // disabled/pending states, a visible focus ring, and a 44x44 hit area under
  // any pointer (drawn by ::before, see ui/styles/elements/toggle.css).
  //
  // Two modes:
  //   - control (default): type="button", bindable `checked`, fires onchange.
  //   - form submit: type="submit" for a server-form master toggle
  //     (@bagel/kit MasterToggle); the parent form owns the state flip, so a
  //     click submits WITHOUT a local pre-toggle. Flipping here as well would
  //     double-toggle against the optimistic update the form wrapper does.
  //
  // aria-checked and aria-busy are written as explicit strings rather than
  // handed a boolean. Svelte serialises `aria-busy={false}` as
  // aria-busy="false" and Astro drops the attribute entirely; the parity test
  // compares rendered HTML, so the two adapters have to agree on the spelling
  // and not on what each framework thinks a false attribute means.
  let {
    checked = $bindable(false),
    label,
    describedby,
    disabled = false,
    pending = false,
    type = 'button',
    onchange,
    ...rest
  }: {
    checked?: boolean;
    /** Accessible name. Required: a switch with no label is unusable by AT. */
    label: string;
    /** id of visible descriptive text, wired to aria-describedby. */
    describedby?: string;
    disabled?: boolean;
    /** In flight: non-interactive but still reflects its current checked state. */
    pending?: boolean;
    type?: 'button' | 'submit';
    onchange?: (v: boolean) => void;
    [key: string]: unknown;
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
  class="bb-switch"
  role="switch"
  aria-checked={checked ? 'true' : 'false'}
  aria-label={label}
  aria-describedby={describedby}
  aria-busy={pending ? 'true' : undefined}
  disabled={disabled || pending}
  data-state={checked ? 'on' : 'off'}
  data-pending={pending ? '' : undefined}
  onclick={flip}
  {...rest}
></button>
