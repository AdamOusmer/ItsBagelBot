<script lang="ts">
    // Copyright (c) 2026 Adam Ousmer. All rights reserved.
    // Proprietary. No license granted. See LICENSE.md.

    // Svelte adapter for the custom cursor. Everything it knows is: how to get
    // two elements onto the page, and when Svelte says to tear down. The
    // physics are in @bagel/ui/lib/cursor-engine and are shared, byte for byte,
    // with the Astro adapter next door.
    //
    // `enabled` is a plain prop rather than a store read, and that is the
    // ports-and-adapters line: whether this visitor wants a custom cursor is a
    // PRODUCT question (the console keeps it in kit's `customCursor`
    // preference store, backed by a per-user setting; the marketing site has no
    // such preference and simply always mounts). A design library that imported
    // that store would be importing the bot.
    import { mountCursor } from '../lib/cursor-engine';
    import '../styles/elements/cursor.css';

    let { enabled = true }: { enabled?: boolean } = $props();

    let dot = $state<HTMLDivElement>();
    let ring = $state<HTMLDivElement>();

    // Effects run after the DOM is patched, so both nodes exist in the same
    // flush that renders them; the guard is for the types and for the disabled
    // case. Returning the engine's teardown makes the flip live in both
    // directions: turning the preference off removes the listeners and the
    // `cursor: none` class with no reload.
    $effect(() => {
        if (!enabled || !dot || !ring) return;
        return mountCursor({ dot, ring });
    });
</script>

{#if enabled}
    <div class="bb-cursor" aria-hidden="true" bind:this={dot}></div>
    <div class="bb-cursor-ring" aria-hidden="true" bind:this={ring}></div>
{/if}
