<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Button from './Button.svelte';
  import Bolota from './Bolota.svelte';
  let { name, role }: { name: string; role: string } = $props();

  // Wakes the Bolota engine while the pointer is over the account row.
  let hovered = $state(false);
</script>

<div class="side-foot">
  <div class="account" role="group" onpointerenter={() => (hovered = true)} onpointerleave={() => (hovered = false)}>
    <div class="avatar"><Bolota name={name} size={34} active={hovered} /></div>
    <div class="who">
      <b>{name}</b>
      <span>{role}</span>
    </div>
  </div>
  <form method="POST" action="/auth/logout" onsubmit={() => localStorage.removeItem('bb-onboarded')}>
    <Button variant="ghost" type="submit" icon="power" style="width:100%;justify-content:center;margin-top:10px">
      Log out
    </Button>
  </form>
</div>

<style>
  .side-foot { border-top: 1px solid var(--glass-border); padding-top: 14px; margin-top: 10px; }
  .account { display: flex; align-items: center; gap: 10px; padding: 6px 8px; }
  .avatar { width: 34px; height: 34px; border-radius: 50%; flex-shrink: 0;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan)); position: relative;
    display: flex; align-items: center; justify-content: center; }
  .who { line-height: 1.2; min-width: 0; }
  .who b { font-size: 13px; font-weight: 600; color: var(--bb-white); display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .who span { font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; color: var(--bb-tan); }
</style>
