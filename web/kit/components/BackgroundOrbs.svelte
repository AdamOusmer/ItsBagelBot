<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The console's ambient backdrop: a faint evergreen field low in the frame and
  // a warm tan one high on the opposite side. Restrained on purpose -- the rules
  // and type do the talking.
  //
  // These lived as literal <div>s in each app's app.html, which is why both
  // consoles had to remember to paste them in, and why the login page has to
  // reach them with a :global rule. Rendered here instead they are shell markup
  // like everything else in RootShell: one copy, and a new app gets them by
  // mounting the shell.
  //
  // Rendered as the shell's FIRST nodes so they stay behind the app content in
  // paint order without needing a negative z-index (which would sink them under
  // the page background). They are position:fixed and pointer-events:none, so
  // they take part in no layout and swallow no clicks.
</script>

<div class="bg-orb"></div>
<div class="bg-orb two"></div>

<style>
  .bg-orb {
    position: fixed; pointer-events: none; z-index: 0;
    top: auto; bottom: -30%; left: -10%; width: min(900px, 90vw); aspect-ratio: 1.6; border-radius: 50%;
    filter: blur(110px); opacity: 0.16;
    background: radial-gradient(ellipse at 40% 60%, rgba(45,106,79,0.55), transparent 65%);
    animation: none;
  }
  .bg-orb.two {
    top: -25%; bottom: auto; left: auto; right: -8%; width: min(640px, 70vw);
    background: radial-gradient(circle at 50% 50%, rgba(201,168,124,0.28), transparent 60%);
    opacity: 0.1;
  }
</style>
