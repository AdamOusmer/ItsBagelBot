<script lang="ts">
    // Copyright (c) 2026 Adam Ousmer. All rights reserved.
    // Proprietary. No license granted. See LICENSE.md.

    import { mountCursor } from '../lib/cursor-engine';
    import '../styles/elements/cursor.css';

    let { enabled = true }: { enabled?: boolean } = $props();

    let dot = $state<HTMLDivElement>();
    let ring = $state<HTMLDivElement>();
    let fill = $state<HTMLDivElement>();

    $effect(() => {
        if (!enabled || !dot || !ring || !fill) return;
        return mountCursor({ dot, ring, fill });
    });
</script>

{#if enabled}
    <div class="bb-cursor" aria-hidden="true" bind:this={dot}></div>
    <div class="bb-cursor-fill" aria-hidden="true" bind:this={fill}></div>
    <div class="bb-cursor-ring" aria-hidden="true" bind:this={ring}>
        <svg aria-hidden="true"><rect width="100%" height="100%" vector-effect="non-scaling-stroke" /></svg>
    </div>
{/if}
