// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterEach, describe, expect, test } from 'bun:test';
import { mountLangSwitch } from '../lib/lang-switch';
import { eventTarget, snapshotGlobals } from './dom-fakes';

afterEach(snapshotGlobals(['window', 'document']));

interface Box {
  width: number;
  height: number;
}

interface Scene {
  field: boolean;
  trigger: { top: number; left: number } & Box;
  menu: Box;
  viewport?: Box;
}

const roomy = { field: false, trigger: { top: 20, left: 1000, width: 60, height: 32 }, menu: { width: 208, height: 240 } };

function openedMenu({ field, trigger, menu, viewport = { width: 1280, height: 800 } }: Scene) {
  let open = false;
  const triggerEl = {
    ...eventTarget(),
    getBoundingClientRect: () => ({ ...trigger, bottom: trigger.top + trigger.height, right: trigger.left + trigger.width }),
  };
  const menuEl = {
    ...eventTarget(),
    style: {} as Record<string, string>,
    dataset: {} as Record<string, string>,
    showPopover: () => {
      open = true;
    },
    matches: () => open,
    getBoundingClientRect: () => menu,
  };
  const root = {
    ...eventTarget(),
    classList: { contains: (name: string) => field && name === 'bb-lang-switch--field' },
    querySelector: (selector: string) => (selector.endsWith('trigger') ? triggerEl : menuEl),
  };
  globalThis.window = { ...eventTarget(), innerHeight: viewport.height } as never;
  globalThis.document = { documentElement: { clientWidth: viewport.width } } as never;
  mountLangSwitch(root as never);
  triggerEl.dispatch('click', { preventDefault: () => {} });
  return { left: menuEl.style.left, top: menuEl.style.top, maxHeight: menuEl.style.maxHeight, side: menuEl.dataset.side };
}

describe('language switch menu placement', () => {
  test.each([
    { name: 'opens below, right edges aligned, when there is room', scene: roomy, want: { left: '852px', top: '60px', side: 'bottom', maxHeight: '732px' } },
    { name: 'field trigger aligns left edges', scene: { ...roomy, field: true, trigger: { top: 20, left: 100, width: 220, height: 40 } }, want: { left: '100px' } },
    { name: 'flips above a trigger near the bottom of the viewport', scene: { ...roomy, field: true, trigger: { top: 700, left: 100, width: 60, height: 32 } }, want: { side: 'top', top: `${700 - 8 - 240}px` } },
    { name: 'stays below when neither side fits and below has more room, capped to it', scene: { ...roomy, field: true, trigger: { top: 60, left: 100, width: 60, height: 32 }, menu: { width: 208, height: 900 } }, want: { side: 'bottom', maxHeight: `${800 - 92 - 16}px` } },
    { name: 'clamps inside the left viewport edge', scene: { ...roomy, trigger: { top: 20, left: 10, width: 60, height: 32 } }, want: { left: '8px' } },
    { name: 'clamps inside the right viewport edge', scene: { ...roomy, field: true, trigger: { top: 20, left: 1250, width: 60, height: 32 } }, want: { left: `${1280 - 8 - 208}px` } },
  ])('$name', ({ scene, want }) => {
    expect(openedMenu(scene)).toMatchObject(want);
  });
});
