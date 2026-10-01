// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterEach, describe, expect, test } from 'bun:test';
import { focusWithin, isRendered, overlayContains, registerOverlayAnchor, trapFocus, wirePopupAnchor } from '../lib/overlay-stack';
import { wireTooltip } from '../lib/tooltip';
import { snapshotGlobals } from './dom-fakes';

type AttrName = string;
type EventType = string;
type KeyName = string;

class FakeElement {
  attrs = new Map<string, string>();
  listeners = new Map<string, (event: never) => void>();
  focused = false;
  focusOptions: unknown;
  hovered = false;
  focusWithinNow = false;
  children: FakeElement[] = [];
  id = '';
  checkVisibility?: () => boolean;
  constructor(
    public offsetParent: unknown = {},
    visibility?: boolean,
    public rects = 1,
  ) {
    if (visibility !== undefined) this.checkVisibility = () => visibility;
  }
  getClientRects() {
    return { length: this.rects };
  }
  hasAttribute(name: AttrName) {
    return this.attrs.has(name);
  }
  getAttribute(name: AttrName) {
    return this.attrs.get(name) ?? null;
  }
  setAttribute(name: AttrName, value: string) {
    this.attrs.set(name, value);
  }
  removeAttribute(name: AttrName) {
    this.attrs.delete(name);
  }
  matches(selector: string) {
    return selector === ':hover' ? this.hovered : selector === ':focus-within' ? this.focusWithinNow : this.hovered || this.focusWithinNow;
  }
  contains(node: unknown) {
    return node === this || this.children.includes(node as FakeElement);
  }
  querySelector() {
    return this.children[0] ?? null;
  }
  querySelectorAll() {
    return this.children;
  }
  addEventListener(type: EventType, fn: (event: never) => void) {
    this.listeners.set(type, fn);
  }
  removeEventListener(type: EventType) {
    this.listeners.delete(type);
  }
  focus(options?: unknown) {
    this.focused = true;
    this.focusOptions = options;
    fakeDocument.activeElement = this;
  }
}

const fakeDocument = { activeElement: null as unknown, listeners: new Map<string, (event: never) => void>() } as {
  activeElement: unknown;
  listeners: Map<string, (event: never) => void>;
  addEventListener(type: string, fn: (event: never) => void): void;
  removeEventListener(type: string): void;
  contains(node: unknown): boolean;
};
fakeDocument.addEventListener = (type, fn) => fakeDocument.listeners.set(type, fn);
fakeDocument.removeEventListener = (type) => fakeDocument.listeners.delete(type);
fakeDocument.contains = () => true;

const restoreGlobals = snapshotGlobals(['document', 'requestAnimationFrame', 'cancelAnimationFrame']);
afterEach(() => {
  restoreGlobals();
  fakeDocument.activeElement = null;
  fakeDocument.listeners.clear();
});

const asEl = (el: FakeElement) => el as unknown as HTMLElement;

describe('trapFocus visibility', () => {
  test.each([
    ['a fixed-position control has no offsetParent yet stays in the trap', [null, true], true],
    ['checkVisibility false drops the control even with an offsetParent', [{}, false], false],
    ['without checkVisibility, client rects keep a control', [null, undefined, 1], true],
    ['without checkVisibility, no client rects drop a control', [{}, undefined, 0], false],
  ] as const)('%s', (_name, args, expected) => {
    expect(isRendered(asEl(new FakeElement(...args)))).toBe(expected);
  });

  test('Tab from the last control wraps to a fixed-position first control', () => {
    globalThis.document = fakeDocument as unknown as Document;
    globalThis.requestAnimationFrame = (() => 0) as never;
    globalThis.cancelAnimationFrame = (() => {}) as never;
    const fixed = new FakeElement(null, true);
    const last = new FakeElement({}, true);
    const node = new FakeElement();
    node.children = [fixed, last];
    trapFocus(asEl(node));
    last.focus();
    let prevented = false;
    node.listeners.get('keydown')?.({ key: 'Tab', shiftKey: false, preventDefault: () => (prevented = true) } as never);
    expect(prevented).toBe(true);
    expect(fakeDocument.activeElement).toBe(fixed);
  });
});

describe('popup anchor wiring', () => {
  test('sets popup state and controls only while open, and cleans up what it set', () => {
    const anchor = new FakeElement();
    const off = wirePopupAnchor(asEl(anchor), true, 'panel-1');
    expect(anchor.attrs.get('aria-haspopup')).toBe('dialog');
    expect(anchor.attrs.get('aria-expanded')).toBe('true');
    expect(anchor.attrs.get('aria-controls')).toBe('panel-1');
    off();
    expect(anchor.attrs.size).toBe(0);
    wirePopupAnchor(asEl(anchor), false, 'panel-1');
    expect(anchor.attrs.get('aria-expanded')).toBe('false');
    expect(anchor.attrs.has('aria-controls')).toBe(false);
  });

  test('attributes the caller already owns are left alone', () => {
    const anchor = new FakeElement();
    anchor.attrs.set('aria-controls', 'list');
    anchor.attrs.set('aria-haspopup', 'listbox');
    wirePopupAnchor(asEl(anchor), true, 'panel-1')();
    expect(anchor.attrs.get('aria-controls')).toBe('list');
    expect(anchor.attrs.get('aria-haspopup')).toBe('listbox');
  });

  test('focusWithin focuses the first control without scrolling, else the container', () => {
    const container = new FakeElement();
    const control = new FakeElement();
    container.children = [control];
    focusWithin(asEl(container));
    expect(control.focused).toBe(true);
    expect(control.focusOptions).toEqual({ preventScroll: true });
    const bare = new FakeElement();
    focusWithin(asEl(bare));
    expect(bare.focused).toBe(true);
  });
});

describe('tooltip wiring', () => {
  function setup(described?: string) {
    globalThis.document = fakeDocument as unknown as Document;
    const root = new FakeElement();
    const trigger = new FakeElement();
    if (described) trigger.attrs.set('aria-describedby', described);
    root.children = [trigger];
    const bubble = new FakeElement();
    bubble.id = 'tip';
    const off = wireTooltip(asEl(root), asEl(bubble));
    const press = (key: KeyName) => {
      let stopped = false;
      fakeDocument.listeners.get('keydown')?.({ key, stopImmediatePropagation: () => (stopped = true) } as never);
      return stopped;
    };
    return { root, trigger, off, press };
  }

  test('the trigger is described by the bubble and the link is removed on teardown', () => {
    const { trigger, off } = setup();
    expect(trigger.attrs.get('aria-describedby')).toBe('tip');
    off();
    expect(trigger.attrs.has('aria-describedby')).toBe(false);
  });

  test('an existing description is kept alongside the tooltip', () => {
    const { trigger, off } = setup('hint');
    expect(trigger.attrs.get('aria-describedby')).toBe('hint tip');
    off();
    expect(trigger.attrs.get('aria-describedby')).toBe('hint');
  });

  test('Escape dismisses a shown tooltip and is consumed once', () => {
    const { root, press } = setup();
    root.focusWithinNow = true;
    expect(press('Escape')).toBe(true);
    expect(root.attrs.has('data-dismissed')).toBe(true);
    expect(press('Escape')).toBe(false);
  });

  test('Escape passes through when the tooltip is not shown', () => {
    const { root, press } = setup();
    expect(press('Escape')).toBe(false);
    expect(root.attrs.has('data-dismissed')).toBe(false);
  });

  test('other keys never dismiss', () => {
    const { root, press } = setup();
    root.hovered = true;
    expect(press('Enter')).toBe(false);
  });

  test('leaving hover and focus makes it show again', () => {
    const { root } = setup();
    root.hovered = true;
    root.attrs.set('data-dismissed', '');
    root.hovered = false;
    root.listeners.get('pointerleave')?.({} as never);
    expect(root.attrs.has('data-dismissed')).toBe(false);
  });

  test('pointer leaving while focus is inside keeps it dismissed', () => {
    const { root } = setup();
    root.focusWithinNow = true;
    root.attrs.set('data-dismissed', '');
    root.listeners.get('pointerleave')?.({} as never);
    expect(root.attrs.has('data-dismissed')).toBe(true);
  });
});

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

describe('overlay containment', () => {
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
});
