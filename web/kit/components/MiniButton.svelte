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
  // Icon HAS since moved into the library, which was the blocker recorded here
  // before this PR, so the import below is the library's. What keeps the
  // wrapper itself in kit is now a different and smaller thing: it emits no
  // markup @bagel/ui does not already emit. `<Button variant="icon" size="sm"
  // icon={...} />` is the same element, so moving this file would add a second
  // name for one contract to the library rather than moving a contract into
  // it. It stays as a kit-local alias for the ~40 console call sites that spell
  // it `MiniButton`, and it is deleted, not moved, when they are renamed.
  //
  // The raw `.mini` rule stays in console.css: 13 markup sites still write
  // `class="mini"` by hand, and they are not this PR's to migrate.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  let { icon, onclick, class: cls = '', ...rest }:
    { icon: IconName; onclick?: () => void; class?: string; [key: string]: unknown } = $props();
</script>

<button class="bb-btn bb-btn--icon bb-btn--sm {cls}" data-mark="" {onclick} {...rest}><Icon name={icon} size={15} /></button>
