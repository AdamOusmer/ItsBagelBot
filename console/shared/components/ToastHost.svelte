<script lang="ts">
  import Icon from './Icon.svelte';
  import { toasts, dismissToast, type ToastItem } from '../lib/toast';

  function undo(t: ToastItem) {
    t.onUndo?.();
    dismissToast(t.id);
  }

  const icon = (kind: ToastItem['kind']) => (kind === 'ok' ? 'check' : kind === 'err' ? 'ban' : 'pulse');
</script>

{#if $toasts.length}
  <div class="toast-stack" aria-live="polite">
    {#each $toasts as t (t.id)}
      <div class="toast {t.kind}" role="status">
        <span class="glyph"><Icon name={icon(t.kind)} size={15} /></span>
        <span class="text">{t.text}</span>
        {#if t.onUndo}
          <button class="undo" type="button" onclick={() => undo(t)}>{t.undoLabel ?? 'Undo'}</button>
        {/if}
        <button class="close" type="button" aria-label="Dismiss" onclick={() => dismissToast(t.id)}>
          <Icon name="x" size={12} />
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-stack {
    position: fixed;
    right: 20px;
    bottom: 20px;
    z-index: 300;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: min(420px, calc(100vw - 32px));
  }

  .toast {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 12px 14px;
    border-radius: 8px 8px;
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border-strong);
    box-shadow: 0 14px 40px rgba(0, 0, 0, 0.5);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white);
    animation: toast-in 220ms var(--bb-ease-out-back, ease-out);
  }
  .toast.ok { border-color: rgba(82, 183, 136, 0.45); color: var(--bb-green-glow); }
  .toast.err { border-color: rgba(176, 90, 70, 0.45); color: #cf8a78; }
  .toast.info { border-color: var(--bb-border-strong); color: var(--bb-tan-light); }

  .text { flex: 1; min-width: 0; }

  .undo {
    flex: none;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: 999px;
    padding: 4px 12px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .undo:hover { background: rgba(201, 168, 124, 0.2); color: var(--bb-white); }

  .close {
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border: none;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    border-radius: 8px;
  }
  .close:hover { color: var(--bb-white); background: rgba(255, 255, 255, 0.06); }

  @keyframes toast-in {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  /* the icon lands a beat after the card: small pop, big read */
  .glyph { display: inline-flex; animation: glyph-pop 320ms var(--bb-ease-out-back, ease-out) 80ms both; }
  @keyframes glyph-pop {
    from { transform: scale(0.4); opacity: 0; }
    to { transform: scale(1); opacity: 1; }
  }
  @media (prefers-reduced-motion: reduce) {
    .glyph { animation: none; }
  }

  @media (max-width: 480px) {
    .toast-stack { left: 16px; right: 16px; bottom: 16px; max-width: none; }
  }
</style>
