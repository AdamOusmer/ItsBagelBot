<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-bg-orb` contract
  // (../styles/elements/bg-orbs.css). No Astro twin: the console shell is
  // Svelte.
  //
  // The console shell's ambient backdrop: a faint evergreen field low in the
  // frame and a warm tan one high on the opposite side.
  //
  // These were literal <div>s in each app's app.html, which is why both
  // consoles had to remember to paste them in and why the login page reached
  // them with a :global rule. Rendered here they are shell markup like
  // everything else: one copy, and a new app gets them by mounting the shell.
  //
  // Rendered as the shell's FIRST nodes so they stay behind the app content in
  // paint order without a negative z-index, which would sink them under the
  // page background instead.
  import '../styles/orbs.css';
  import '../styles/elements/bg-orbs.css';
</script>

<div class="bb-orb bb-orb--fixed bb-orb--wash-green bb-bg-orb"></div><div
  class="bb-orb bb-orb--fixed bb-orb--wash-tan bb-bg-orb bb-bg-orb--two"
></div>
