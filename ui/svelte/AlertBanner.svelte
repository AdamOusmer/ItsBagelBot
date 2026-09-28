<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/alert.css';
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import { alertClass, type AlertAction, type AlertLook } from '../lib/alert';

  type Props = Omit<HTMLAttributes<HTMLDivElement>, 'role'> &
    AlertLook & {
      role?: 'alert' | 'status' | 'note';
      action?: AlertAction;
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
    action,
    class: className,
    children,
    actions,
    ...rest
  }: Props = $props();
</script>

<div class={[alertClass({ tone, variant, placement, row, flush, stack }), className]} {role} {...rest}><span class="bb-alert__msg"
    >{@render children?.()}</span
  >{@render actions?.()}{#if action && 'formAction' in action}<form method="POST" action={action.formAction}
      ><button type="submit" class="bb-alert__action">{action.label}</button></form
    >{:else if action}<a class="bb-alert__action" href={action.href}>{action.label}</a>{/if}</div>
