<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/alert.css';
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import { alertClass, type AlertCta, type AlertLook } from '../lib/alert';

  type Props = Omit<HTMLAttributes<HTMLDivElement>, 'role'> &
    AlertLook & {
      role?: 'alert' | 'status' | 'note';
      cta?: AlertCta;
      actions?: Snippet;
    };

  let {
    tone,
    variant,
    placement,
    row,
    flush,
    stack,
    role = 'alert',
    cta,
    class: className,
    children,
    actions,
    ...rest
  }: Props = $props();
</script>

<div class={[alertClass({ tone, variant, placement, row, flush, stack }), className]} {role} {...rest}><span class="bb-alert__msg"
    >{@render children?.()}</span
  >{@render actions?.()}{#if cta && 'formAction' in cta}<form method="POST" action={cta.formAction}
      ><button type="submit" class="bb-alert__action">{cta.label}</button></form
    >{:else if cta}<a class="bb-alert__action" href={cta.href}>{cta.label}</a>{/if}</div>
