<script lang="ts">
  let { perm }: { perm: 'everyone' | 'sub' | 'mod' | 'broadcaster' } = $props();
  const label = { everyone: 'Everyone', sub: 'Subs', mod: 'Mods', broadcaster: 'Broadcaster' };
</script>

<span class="badge {perm}">{label[perm]}</span>
