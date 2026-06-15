<script lang="ts">
  // Heavy ambient motion for the onboarding experience: several large blurred
  // brand orbs each drifting on their own loop, a slowly rotating gradient mesh,
  // and pointer parallax layered on top. Pure CSS keyframes drive the continuous
  // motion (GPU transforms); a single rAF adds the parallax offset. Honors
  // prefers-reduced-motion by freezing to a static gradient.
  import { onMount } from 'svelte';

  let root: HTMLDivElement;

  onMount(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      root.dataset.static = 'true';
      return;
    }
    const fine = window.matchMedia('(pointer: fine)').matches;
    if (!fine) return;

    let tx = 0,
      ty = 0,
      cx = 0,
      cy = 0,
      raf = 0;
    const onMove = (e: PointerEvent) => {
      tx = (e.clientX / window.innerWidth - 0.5) * 2;
      ty = (e.clientY / window.innerHeight - 0.5) * 2;
      if (!raf) raf = requestAnimationFrame(loop);
    };
    const loop = () => {
      cx += (tx - cx) * 0.06;
      cy += (ty - cy) * 0.06;
      root.style.setProperty('--px', cx.toFixed(3));
      root.style.setProperty('--py', cy.toFixed(3));
      if (Math.abs(tx - cx) > 0.001 || Math.abs(ty - cy) > 0.001) raf = requestAnimationFrame(loop);
      else raf = 0;
    };
    window.addEventListener('pointermove', onMove, { passive: true });
    return () => {
      if (raf) cancelAnimationFrame(raf);
      window.removeEventListener('pointermove', onMove);
    };
  });
</script>

<div class="aurora" bind:this={root}>
  <div class="mesh"></div>
  <span class="orb o1"></span>
  <span class="orb o2"></span>
  <span class="orb o3"></span>
  <span class="orb o4"></span>
  <div class="grain"></div>
</div>

<style>
  .aurora {
    --px: 0;
    --py: 0;
    position: fixed;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    pointer-events: none;
    background: var(--bb-black, #0a0a0a);
  }

  /* slowly rotating conic mesh, very low opacity */
  .mesh {
    position: absolute;
    inset: -40%;
    background: conic-gradient(
      from 0deg at 50% 50%,
      rgba(45, 106, 79, 0.18),
      rgba(201, 168, 124, 0.12),
      rgba(82, 183, 136, 0.16),
      rgba(10, 10, 10, 0) 70%,
      rgba(45, 106, 79, 0.18)
    );
    filter: blur(40px);
    opacity: 0.7;
    animation: spin 48s linear infinite;
    transform: translate(calc(var(--px) * -14px), calc(var(--py) * -14px));
  }

  .orb {
    position: absolute;
    border-radius: 50%;
    filter: blur(70px);
    will-change: transform;
  }
  .o1 {
    top: -12%;
    left: 8%;
    width: min(640px, 64vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 38% 38%, rgba(82, 183, 136, 0.5), rgba(45, 106, 79, 0.25) 45%, transparent 70%);
    opacity: 0.6;
    animation: drift1 19s ease-in-out infinite;
  }
  .o2 {
    bottom: -16%;
    right: 4%;
    width: min(560px, 58vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, rgba(201, 168, 124, 0.4), transparent 64%);
    opacity: 0.55;
    animation: drift2 26s ease-in-out infinite;
  }
  .o3 {
    top: 30%;
    right: 24%;
    width: min(420px, 42vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, rgba(82, 183, 136, 0.32), transparent 66%);
    opacity: 0.5;
    animation: drift3 22s ease-in-out infinite;
  }
  .o4 {
    bottom: 8%;
    left: 18%;
    width: min(360px, 40vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, rgba(224, 196, 154, 0.3), transparent 64%);
    opacity: 0.4;
    animation: drift4 30s ease-in-out infinite;
  }

  /* parallax: deeper orbs move more, layered via the shared --px/--py */
  .o1 { transform: translate(calc(var(--px) * 26px), calc(var(--py) * 26px)); }
  .o2 { transform: translate(calc(var(--px) * -34px), calc(var(--py) * -30px)); }
  .o3 { transform: translate(calc(var(--px) * 40px), calc(var(--py) * -22px)); }
  .o4 { transform: translate(calc(var(--px) * -20px), calc(var(--py) * 28px)); }

  .grain {
    position: absolute;
    inset: 0;
    opacity: 0.05;
    background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='1'/%3E%3C/svg%3E");
  }

  @keyframes spin { to { rotate: 360deg; } }
  @keyframes drift1 { 0%, 100% { translate: 0 0; scale: 1; } 50% { translate: 5% 6%; scale: 1.1; } }
  @keyframes drift2 { 0%, 100% { translate: 0 0; scale: 1; } 50% { translate: -6% -4%; scale: 1.12; } }
  @keyframes drift3 { 0%, 100% { translate: 0 0; } 50% { translate: -7% 8%; } }
  @keyframes drift4 { 0%, 100% { translate: 0 0; scale: 1; } 50% { translate: 6% -7%; scale: 1.08; } }

  /* data-static is toggled from JS, so globalize the state part of the selector */
  .aurora:global([data-static='true']) .mesh,
  .aurora:global([data-static='true']) .orb { animation: none; }
</style>
