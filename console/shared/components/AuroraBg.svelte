<script lang="ts">
  // Ambient motion background for the onboarding. Pure CSS — a few blurred brand
  // orbs drifting on independent loops. No JS, no conic mesh, modest blur: keeps
  // it cheap so login/logout stay smooth. Freezes for reduced-motion.
</script>

<div class="aurora" aria-hidden="true">
  <span class="orb o1"></span>
  <span class="orb o2"></span>
  <span class="orb o3"></span>
  <div class="grain"></div>
</div>

<style>
  .aurora {
    position: fixed;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    pointer-events: none;
    background: var(--bb-black, #0a0a0a);
  }
  .orb {
    position: absolute;
    border-radius: 50%;
    filter: blur(56px);
    will-change: transform;
  }
  .o1 {
    top: -10%;
    left: 6%;
    width: min(560px, 60vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 38% 38%, rgba(82, 183, 136, 0.45), rgba(45, 106, 79, 0.2) 46%, transparent 70%);
    opacity: 0.55;
    animation: drift1 24s ease-in-out infinite;
  }
  .o2 {
    bottom: -14%;
    right: 4%;
    width: min(480px, 54vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, rgba(201, 168, 124, 0.36), transparent 64%);
    opacity: 0.5;
    animation: drift2 30s ease-in-out infinite;
  }
  .o3 {
    top: 34%;
    right: 26%;
    width: min(360px, 40vw);
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, rgba(82, 183, 136, 0.28), transparent 66%);
    opacity: 0.4;
    animation: drift3 26s ease-in-out infinite;
  }
  .grain {
    position: absolute;
    inset: 0;
    opacity: 0.05;
    background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='1'/%3E%3C/svg%3E");
  }
  @keyframes drift1 { 0%, 100% { transform: translate(0, 0) scale(1); } 50% { transform: translate(4%, 5%) scale(1.08); } }
  @keyframes drift2 { 0%, 100% { transform: translate(0, 0) scale(1); } 50% { transform: translate(-5%, -3%) scale(1.1); } }
  @keyframes drift3 { 0%, 100% { transform: translate(0, 0); } 50% { transform: translate(-6%, 7%); } }
  @media (prefers-reduced-motion: reduce) {
    .orb { animation: none; }
  }
</style>
