// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type DiscardGuard = {
  readonly open: boolean;
  guard: (action: () => void) => void;
  confirm: () => void;
  cancel: () => void;
};

export function createDiscardGuard(dirty: () => boolean, onDiscard?: () => void): DiscardGuard {
  let open = $state(false);
  let parked: (() => void) | null = null;

  return {
    get open() {
      return open;
    },
    guard(action: () => void) {
      if (!dirty()) {
        action();
        return;
      }
      parked = action;
      open = true;
    },
    confirm() {
      open = false;
      onDiscard?.();
      const replay = parked;
      parked = null;
      replay?.();
    },
    cancel() {
      open = false;
      parked = null;
    }
  };
}
