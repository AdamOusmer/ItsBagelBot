// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, test } from 'bun:test';
import { mountScrollSpy, mountSectionNav } from '../lib/scroll-spy';
import { snapshotGlobals } from './dom-fakes';

afterEach(snapshotGlobals(['window', 'document', 'ResizeObserver', 'MutationObserver', 'requestAnimationFrame', 'cancelAnimationFrame']));

function fixture() {
  const events = Object.assign(new EventTarget(), {
    innerHeight: 1000,
    getComputedStyle: () => ({ scrollMarginTop: '160px', overflowY: 'auto' }),
  });
  const scroller = { scrollTop: 100, clientHeight: 1000, scrollHeight: 3000 };
  const frames = new Map<number, FrameRequestCallback>();
  let nextFrame = 0;
  const observers: { callback: () => void; disconnected: boolean; targets: unknown[] }[] = [];
  class Observer {
    disconnected = false;
    targets: unknown[] = [];
    constructor(readonly callback: () => void) { observers.push(this); }
    observe(target: unknown) { this.targets.push(target); }
    disconnect() { this.disconnected = true; }
  }
  const entries = [
    { id: 'first', top: -500, visible: true },
    { id: 'second', top: 600, visible: true },
    { id: 'last', top: 900, visible: true },
  ];
  const sections = entries.map((entry) => Object.assign(entry, {
    getClientRects: () => entry.visible ? [{}] : [],
    getBoundingClientRect: () => ({ top: entry.top }),
  })) as unknown as HTMLElement[];
  const active = new Set<string>();
  const aria = new Map<string, string>();
  const links = new Map(entries.map(({ id }) => [id, {
    hash: `#${id}`,
    classList: {
      toggle: (_class: string, on: boolean) => on ? active.add(id) : active.delete(id),
      remove: () => active.delete(id),
    },
    setAttribute: (_attr: string, value: string) => aria.set(id, value),
    removeAttribute: () => aria.delete(id),
    getBoundingClientRect: () => ({ top: 310, bottom: 350 }),
  } as unknown as HTMLElement]));
  const globals = {
    window: events,
    document: {
      scrollingElement: scroller,
      documentElement: {},
      getElementById: (id: string) => sections.find((section) => section.id === id),
    },
    ResizeObserver: Observer,
    MutationObserver: Observer,
    requestAnimationFrame: (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame; },
    cancelAnimationFrame: (id: number) => frames.delete(id),
  };
  for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { configurable: true, value });
  function flush() {
    for (const [id, callback] of frames) { frames.delete(id); callback(0); }
  }
  function dispatch(event = 'scroll') { events.dispatchEvent(new Event(event)); flush(); }
  return { events, scroller, frames, observers, entries, sections, links, active, aria, flush, dispatch };
}

test('coalesces scrolling, follows sections in both directions, and fully disposes before a route swap', () => {
  const f = fixture();
  const dispose = mountScrollSpy(f.sections, f.links);
  expect([...f.active]).toEqual(['first']);
  expect(f.aria.get('first')).toBe('location');
  f.entries[1].top = 300;
  f.events.dispatchEvent(new Event('scroll'));
  f.events.dispatchEvent(new Event('resize'));
  expect(f.frames.size).toBe(1);
  f.flush();
  expect([...f.active]).toEqual(['second']);
  expect([...f.aria]).toEqual([['second', 'location']]);
  f.entries[1].top = 600;
  f.dispatch();
  expect([...f.active]).toEqual(['first']);
  f.events.dispatchEvent(new Event('scroll'));
  const pending = [...f.frames.values()][0];
  dispose();
  expect(f.frames.size).toBe(0);
  expect(f.observers.every((observer) => observer.disconnected)).toBe(true);
  f.entries[1].top = 300;
  f.dispatch();
  f.dispatch('resize');
  f.dispatch('hashchange');
  expect(f.frames.size).toBe(0);
  pending(0);
  expect([...f.active]).toEqual(['first']);
  dispose();
});

test('the short final section wins at the document bottom, including fractional scroll positions', () => {
  const f = fixture();
  const dispose = mountScrollSpy(f.sections, f.links);
  f.entries[1].top = 100;
  f.entries[2].top = 700; // stays below the 350px activation line
  f.scroller.scrollTop = 1999.5;
  f.dispatch();
  expect([...f.active]).toEqual(['last']);
  f.scroller.scrollTop = 1980;
  f.dispatch();
  expect([...f.active]).toEqual(['second']);
  dispose();
});

test('an unscrollable page keeps its first section active instead of forcing the last', () => {
  const f = fixture();
  f.scroller.scrollTop = 0;
  f.scroller.scrollHeight = f.scroller.clientHeight;
  const dispose = mountScrollSpy(f.sections, f.links);
  expect([...f.active]).toEqual(['first']);
  dispose();
});

test('hidden sections do not win at the bottom, and layout changes update without scrolling', () => {
  const f = fixture();
  f.scroller.scrollTop = 2000;
  f.entries[2].visible = false;
  const dispose = mountScrollSpy(f.sections, f.links);
  expect([...f.active]).toEqual(['second']);
  f.scroller.scrollHeight = 3500;
  f.observers[0].callback();
  f.flush();
  expect([...f.active]).toEqual(['first']);
  f.entries.forEach((entry) => { entry.visible = false; });
  f.observers[0].callback();
  f.flush();
  expect([...f.active]).toEqual([]);
  expect(f.aria.size).toBe(0);
  dispose();
});

test('the active item scrolls into view within a height-limited menu', () => {
  const f = fixture();
  const scrollContainer = {
    scrollTop: 0, scrollHeight: 500, clientHeight: 200,
    getBoundingClientRect: () => ({ top: 100, bottom: 300 }),
  } as HTMLElement;
  const dispose = mountScrollSpy(f.sections, f.links, { scrollContainer });
  expect(scrollContainer.scrollTop).toBe(50);
  scrollContainer.scrollTop = 0;
  f.dispatch();
  expect(scrollContainer.scrollTop).toBe(0);
  dispose();
});

test('a short sidebar containing the menu can scroll the active item without scrolling the page', () => {
  const f = fixture();
  const sidebar = {
    scrollTop: 0, scrollHeight: 650, clientHeight: 200,
    getBoundingClientRect: () => ({ top: 100, bottom: 300 }),
  } as HTMLElement;
  const nav = { scrollHeight: 500, clientHeight: 500, parentElement: sidebar } as HTMLElement;
  const dispose = mountScrollSpy(f.sections, f.links, { scrollContainer: nav });
  expect(sidebar.scrollTop).toBe(50);
  expect(f.scroller.scrollTop).toBe(100);
  sidebar.scrollTop = 0;
  f.observers[0].callback();
  f.flush();
  expect(sidebar.scrollTop).toBe(50);
  dispose();
});

test('section menus use anchor offsets and rebind after filtering or link replacement', () => {
  const f = fixture();
  let renderedLinks = [...f.links.values()];
  const root = {
    querySelectorAll: () => renderedLinks,
    scrollHeight: 200, clientHeight: 200,
  } as unknown as HTMLElement;
  f.entries[1].top = 250; // visible but below the sticky chrome
  const dispose = mountSectionNav(root);
  expect([...f.active]).toEqual(['first']);
  f.entries[1].top = 160;
  f.dispatch('hashchange');
  expect([...f.active]).toEqual(['second']);
  renderedLinks = [f.links.get('last')!];
  f.entries[2].top = 600;
  const linkObserver = f.observers.find((observer) => observer.targets.length === 1 && observer.targets.includes(root))!;
  linkObserver.callback();
  expect(f.aria.get('last')).toBe('location');
  expect(f.observers[0].disconnected).toBe(true);
  renderedLinks = [];
  linkObserver.callback();
  dispose();
  expect(f.observers.every((observer) => observer.disconnected)).toBe(true);
});

test('scroll margins and the menu scroller are read once, then again only after a resize', () => {
  const f = fixture();
  let styleReads = 0;
  Object.assign(f.events, { getComputedStyle: () => { styleReads += 1; return { scrollMarginTop: '160px', overflowY: 'auto' }; } });
  const root = Object.assign({
    scrollTop: 0, scrollHeight: 500, clientHeight: 200,
    getBoundingClientRect: () => ({ top: 100, bottom: 300 }),
  }, { querySelectorAll: () => [...f.links.values()] }) as unknown as HTMLElement;
  const dispose = mountSectionNav(root);
  const afterMount = styleReads;
  expect(afterMount).toBe(f.sections.length + 1);
  f.dispatch();
  f.dispatch();
  f.dispatch('hashchange');
  expect(styleReads).toBe(afterMount);
  f.dispatch('resize');
  expect(styleReads).toBe(afterMount + f.sections.length + 1);
  dispose();
});
