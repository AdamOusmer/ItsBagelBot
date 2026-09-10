<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for a <textarea> inside the `.bb-input` frame
  // (../styles/elements/input.css). Astro twin: ../astro/Textarea.astro.
  //
  // `resize: vertical` is the contract's, not a prop: a textarea a user can
  // drag WIDER escapes its grid cell and pushes the form's layout, and no CSS
  // clamps a manual resize back. The reason is at the rule.
  //
  // `rows` defaults to 3 and is forwarded rather than translated into a
  // height: the attribute is what a no-CSS render falls back to, and the
  // contract's `min-height` is expressed in the same 3 lines so the two agree.
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';

  let {
    value = $bindable(''),
    rows = 3,
    invalid = false,
    mono = false,
    class: className = '',
    ...rest
  }: {
    value?: string;
    rows?: number;
    invalid?: boolean;
    mono?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-input', 'bb-input--area', mono ? 'bb-input--mono' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}><textarea {rows} bind:value {...rest}
  ></textarea></span>
