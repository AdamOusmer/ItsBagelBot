<script lang="ts">
    // Copyright (c) 2026 Adam Ousmer. All rights reserved.
    // Proprietary. No license granted. See LICENSE.md.

    // Svelte adapter for the drifting mote field. Lifecycle only; the physics
    // are in @bagel/ui/lib/light-field, shared with the Astro adapter so the
    // console and itsbagelbot.com cannot drift apart on how the field looks.
    import { onMount } from 'svelte';
    import { field } from '../lib/light-field';
    import '../styles/elements/light-field.css';

    let { class: className = '', warmth = 0.7 }: { class?: string; warmth?: number } = $props();

    let host: HTMLDivElement;

    // field() returns null under reduced motion and without Web Animations;
    // onMount is happy with an undefined teardown, and the contract CSS hides
    // the host in the reduced-motion case anyway.
    onMount(() => field(host, { warmth }) ?? undefined);
</script>

<!-- `data-field` and `data-warmth` are the contract, not this adapter's
     plumbing: the Astro half finds its hosts by scanning for `[data-field]`
     and reads the warmth off the attribute, and adapter parity means the two
     emit the same markup or neither is the contract. Svelte binds the node
     directly and passes `warmth` as a prop, so here they are inert — and they
     are also what a Playwright selector and a future non-framework consumer
     would reach for. -->
<div
    class={['bb-light-field', className].filter(Boolean).join(' ')}
    data-field
    data-warmth={warmth}
    aria-hidden="true"
    bind:this={host}
></div>
