<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // The 28px icon-only button that fills the console's row-action columns.
  //
  // It is no longer its own look: the scoped `.mini` block this carried is now
  // `.bb-btn--icon.bb-btn--sm` in @bagel/ui/styles/elements/button.css, pixel
  // for pixel (28px square, transparent frame at rest, 15px glyph at 1.7
  // stroke, hairline on hover). The divergence that justified a separate
  // element -- an icon button that is invisible until you point at it, where
  // `--icon` at 40px wears its frame all the time -- is recorded there as four
  // declarations instead of as a second component.
  //
  // What keeps this file in kit rather than moving it into ui/svelte with the
  // rest of the button family: it renders Icon, and Icon does not move into the
  // library until the nav/shell PR. Importing @bagel/kit from ui is the one
  // thing scripts/assert-framework-free.mjs refuses outright, so this wrapper
  // waits for Icon and moves with it.
  //
  // The raw `.mini` rule stays in console.css for now: 13 markup sites still
  // write `class="mini"` by hand, and they are not this PR's to migrate.
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let { icon, onclick, class: cls = '', ...rest }:
    { icon: IconName; onclick?: () => void; class?: string; [key: string]: unknown } = $props();
</script>

<button class="bb-btn bb-btn--icon bb-btn--sm {cls}" data-mark="" {onclick} {...rest}><Icon name={icon} size={15} /></button>
