// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterAll, expect, test } from 'bun:test';
import { defineTextFit, fitRatio, fitSlotOf, groupMin } from '../lib/text-fit';
import { snapshotGlobals } from './dom-fakes';

test('fitRatio leaves text that already fits at full size', () => {
  expect(fitRatio({ needed: 200, deficit: 0 })).toBe(1);
  expect(fitRatio({ needed: 0, deficit: 0 })).toBe(1);
});

test('fitRatio scales by the missing share with a 0.99 safety margin', () => {
  expect(fitRatio({ needed: 200, deficit: 50 })).toBeCloseTo(0.7425, 10);
  expect(fitRatio({ needed: 100, deficit: 1 })).toBeCloseTo(0.9801, 10);
});

test('fitRatio never shrinks below half size', () => {
  expect(fitRatio({ needed: 200, deficit: 150 })).toBe(0.5);
  expect(fitRatio({ needed: 200, deficit: 400 })).toBe(0.5);
});

test('groupMin gives every member of a group its smallest ratio', () => {
  expect(
    groupMin([
      { group: 'hero', ratio: 1 },
      { group: undefined, ratio: 0.6 },
      { group: 'hero', ratio: 0.8 },
      { group: 'cta', ratio: 0.9 },
      { group: 'hero', ratio: 0.95 },
    ]),
  ).toEqual([0.8, 0.6, 0.8, 0.9, 0.8]);
});

type Slot = Parameters<typeof fitSlotOf>[0];

test('fitSlotOf reads bb-fit attributes', () => {
  const element = { getAttribute: (name: string) => ({ mode: 'block', group: 'dock' })[name] ?? null };
  expect(fitSlotOf(element as unknown as Slot)).toEqual({ block: true, group: 'dock' });
  const line = { getAttribute: () => null };
  expect(fitSlotOf(line as unknown as Slot)).toEqual({ block: false, group: undefined });
});

// bun test has no DOM, so the element runs against stubs of the platform calls the engine makes.
const restoreGlobals = snapshotGlobals(['HTMLElement', 'customElements', 'ResizeObserver', 'MutationObserver', 'document', 'getComputedStyle']);
afterAll(restoreGlobals);

const observed: string[] = [];
const defined = new Map<string, { new (): StubElement; observedAttributes?: string[] }>();

class StubElement {
  localName = 'bb-fit';
  isConnected = true;
  attrs = new Map<string, string>();
  classes = new Set<string>();
  props = new Map<string, string>();
  scrollWidth = 0;
  clientWidth = 0;
  parentElement = {
    closest: () => null,
    getBoundingClientRect: () => ({ left: 0, right: 100, bottom: 20, width: 100, height: 20 }),
  };
  style = {
    letterSpacing: '',
    setProperty: (name: string, value: string) => void this.props.set(name, value),
    removeProperty: (name: string) => void this.props.delete(name),
  };
  classList = { add: (name: string) => void this.classes.add(name) };
  dataset = {};
  visible = true;
  section: { contains: (node: unknown) => boolean } | null = null;
  checkVisibility = () => this.visible;
  closest = () => this.section;
  getAttribute(name: string) {
    return this.attrs.get(name) ?? null;
  }
  getBoundingClientRect() {
    const ratio = parseFloat(this.props.get('--bb-fit') ?? '1');
    return { left: 0, right: 200 * ratio, bottom: 10, width: 200 * ratio };
  }
  connectedCallback?(): void;
  disconnectedCallback?(): void;
  attributeChangedCallback?(): void;
}

const resizeCallbacks: ((entries: { target: unknown }[]) => void)[] = [];

class StubResizeObserver {
  constructor(callback: (entries: { target: unknown }[]) => void) {
    resizeCallbacks.push(callback);
  }
  observe = (box: unknown) => void observed.push(`observe:${(box as { tag?: string }).tag ?? 'box'}`);
  unobserve = () => void observed.push('unobserve');
  disconnect = () => {};
}

const stateListeners: ((event: unknown) => void)[] = [];
const stubDocument = {
  fonts: { ready: Promise.resolve(), addEventListener() {} },
  addEventListener: (type: string, listener: (event: unknown) => void) => {
    if (type === 'contentvisibilityautostatechange') stateListeners.push(listener);
  },
};

function installStubs() {
  Object.assign(globalThis, {
    HTMLElement: StubElement,
    customElements: {
      get: (name: string) => defined.get(name),
      define: (name: string, ctor: never) => void defined.set(name, ctor),
    },
    ResizeObserver: StubResizeObserver,
    MutationObserver: class {
      observe() {}
      disconnect() {}
    },
    document: stubDocument,
    getComputedStyle: () => ({
      borderLeftWidth: '0px', borderRightWidth: '0px', borderBottomWidth: '0px',
      paddingLeft: '0px', paddingRight: '0px', paddingBottom: '0px', letterSpacing: '0px',
    }),
  });
}

test('defineTextFit registers bb-fit once with mode and group observed', () => {
  installStubs();
  defineTextFit();
  defineTextFit();
  expect([...defined.keys()]).toEqual(['bb-fit']);
  expect(defined.get('bb-fit')?.observedAttributes).toEqual(['mode', 'group']);
});

test('a connected bb-fit fits its parent box, and disconnecting releases the box', async () => {
  installStubs();
  defineTextFit();
  const Ctor = defined.get('bb-fit') as { new (): StubElement };
  const element = new Ctor();
  element.connectedCallback?.();
  expect(observed).toContain('observe:box');
  await Promise.resolve();
  expect(element.props.get('--bb-fit')).toBe('0.5');
  expect(element.classes.has('is-fit')).toBe(true);
  element.disconnectedCallback?.();
  expect(observed.at(-1)).toBe('unobserve');
});

test('an element in a skipped section is not measured until its section arrives', async () => {
  installStubs();
  const Ctor = defined.get('bb-fit') as { new (): StubElement };
  const section = { contains: (node: unknown) => node === section };
  const element = new Ctor();
  element.visible = false;
  element.section = section;
  element.connectedCallback?.();
  await Promise.resolve();
  expect(element.props.has('--bb-fit')).toBe(false);
  expect(element.classes.has('is-fit')).toBe(false);

  for (const listener of stateListeners) listener({ target: section, skipped: true });
  expect(element.classes.has('is-fit')).toBe(false);

  element.visible = true;
  for (const listener of stateListeners) listener({ target: section, skipped: false });
  expect(element.props.get('--bb-fit')).toBe('0.5');
  expect(element.classes.has('is-fit')).toBe(true);
});

test('a visible element fits without any section event', async () => {
  installStubs();
  const Ctor = defined.get('bb-fit') as { new (): StubElement };
  const element = new Ctor();
  element.connectedCallback?.();
  await Promise.resolve();
  expect(stateListeners.length).toBe(1);
  expect(element.classes.has('is-fit')).toBe(true);
});

test('a resize notification refits the box', async () => {
  installStubs();
  const Ctor = defined.get('bb-fit') as { new (): StubElement };
  const element = new Ctor();
  element.connectedCallback?.();
  await Promise.resolve();
  expect(element.props.get('--bb-fit')).toBe('0.5');

  element.props.delete('--bb-fit');
  resizeCallbacks[0]([{ target: element.parentElement }]);
  expect(element.props.get('--bb-fit')).toBe('0.5');
});
