// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The dirty guard four inspector pages (timers, channel points, commands,
// modules/[id]) had each rewritten: close / row-switch / new all route through
// one confirmation instead of silently dropping an in-progress edit.
//
// Template Method. The skeleton is fixed -- park the intent, raise the dialog,
// replay the intent on confirm, drop it on cancel -- and the two varying steps
// are hooks: `dirty()` says whether there is anything to lose, `onDiscard()`
// does whatever else the page throws away with the draft (reset the inspector,
// clear a sessionStorage mirror).
//
// The parked action is cleared BEFORE it runs, not after: the replayed action
// is usually an open, which can itself re-enter the guard, and a stale
// `afterDiscard` at that moment replays the previous intent a second time.
//
// Lives here rather than in @bagel/shared/lib alongside the pure
// inspector-machine because it holds `$state`: shared/lib is framework-free by
// its own header (that is what lets the machine be unit-tested without a
// component harness), and only the dashboard has inspector pages. The machine
// once carried the same idea as a closed set of intents
// (close/select/navigate); nothing used it, because these pages park an
// arbitrary callback, so that half is gone and this is the only parker.

export type DiscardGuard = {
  /** Whether the confirmation dialog is showing. */
  readonly open: boolean;
  /** Run `action` now, or park it behind a confirmation while the draft is dirty. */
  guard: (action: () => void) => void;
  /** User chose to discard: drop the draft and replay the parked action. */
  confirm: () => void;
  /** User chose to keep editing. */
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
