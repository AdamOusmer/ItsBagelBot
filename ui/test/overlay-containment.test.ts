// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { overlayContains, registerOverlayAnchor } from '../lib/overlay-stack';

class ElementNode {
  readonly nodeType = 1;
  constructor(readonly parentElement: ElementNode | null = null) {}
  contains(target: ElementNode | { parentElement: ElementNode | null } | null): boolean {
    for (let node = target; node; node = node.parentElement) {
      if (node === this) return true;
    }
    return false;
  }
}

function element(parent: HTMLElement | null = null): HTMLElement {
  return new ElementNode(parent as unknown as ElementNode) as unknown as HTMLElement;
}

test('normal containment and outside clicks retain their ordinary behavior', () => {
  const panel = element();
  expect(overlayContains(panel, panel)).toBe(true);
  expect(overlayContains(panel, element(panel))).toBe(true);
  expect(overlayContains(panel, element())).toBe(false);
  expect(overlayContains(panel, null)).toBe(false);
});

test('portalled child options and scroll targets belong to their anchor parent', () => {
  const outer = element();
  const trigger = element(outer);
  const portal = element();
  const dialog = element(portal);
  const option = element(dialog);
  const unregister = registerOverlayAnchor(portal, trigger);
  expect(overlayContains(outer, option)).toBe(true);
  expect(overlayContains(outer, dialog)).toBe(true);
  // A mobile scrim is a sibling of the dialog inside the registered shell.
  expect(overlayContains(outer, element(portal))).toBe(true);
  const text = { nodeType: 3, parentElement: option } as unknown as Node;
  expect(overlayContains(outer, text)).toBe(true);
  unregister();
  expect(overlayContains(outer, option)).toBe(false);
});

test('ownership follows multiple nested pickers without accepting unrelated portals', () => {
  const outer = element();
  const child = element();
  const grandchild = element();
  const unrelated = element();
  const unregisterChild = registerOverlayAnchor(child, element(outer));
  const unregisterGrandchild = registerOverlayAnchor(grandchild, element(child));
  const unregisterUnrelated = registerOverlayAnchor(unrelated, element());
  expect(overlayContains(outer, element(grandchild))).toBe(true);
  expect(overlayContains(child, element(grandchild))).toBe(true);
  expect(overlayContains(grandchild, element(child))).toBe(false);
  expect(overlayContains(outer, element(unrelated))).toBe(false);
  unregisterChild();
  expect(overlayContains(outer, element(grandchild))).toBe(false);
  unregisterGrandchild();
  unregisterUnrelated();
});

test('cleanup cannot erase a replacement anchor and cycles terminate outside', () => {
  const outer = element();
  const portal = element();
  const oldCleanup = registerOverlayAnchor(portal, element());
  const cleanup = registerOverlayAnchor(portal, element(outer));
  oldCleanup();
  expect(overlayContains(outer, element(portal))).toBe(true);
  cleanup();
  const first = element();
  const second = element();
  const firstCleanup = registerOverlayAnchor(first, element(second));
  const secondCleanup = registerOverlayAnchor(second, element(first));
  expect(overlayContains(outer, element(first))).toBe(false);
  firstCleanup();
  secondCleanup();
});
