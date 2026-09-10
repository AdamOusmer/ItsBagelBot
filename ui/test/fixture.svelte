<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte half of the parity fixture; see ./fixture.astro for what it is for.
  // The two files are written to look as different as their frameworks make
  // them (runes props vs Astro.props) and to emit identical markup.
  let { text = 'hello' }: { text?: string } = $props();
</script>

<p class="bb-fixture" data-fixture>{text}</p>
