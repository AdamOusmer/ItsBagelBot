// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from "bun:test";

import {
  createNestedScrollGate,
  keepsWheel,
  readScrollBox,
  type ScrollAxis,
  type ScrollBox,
} from "../lib/nested-scroll";

const DOWN = { deltaX: 0, deltaY: 120 };
const UP = { deltaX: 0, deltaY: -120 };
const RIGHT = { deltaX: 120, deltaY: 10 };

function box(over: { x?: Partial<ScrollAxis>; y?: Partial<ScrollAxis> } = {}): ScrollBox {
  return {
    x: { overflow: "visible", overscroll: "auto", at: 0, max: 0, ...over.x },
    y: { overflow: "auto", overscroll: "auto", at: 0, max: 2700, ...over.y },
  };
}

describe("keepsWheel", () => {
  test("a pane with nothing to scroll hands the wheel to the page", () => {
    expect(keepsWheel(box({ y: { max: 0 } }), DOWN)).toBe(false);
    expect(keepsWheel(box({ y: { max: -150 } }), DOWN)).toBe(false);
  });

  test("an element that does not scroll on the gesture's axis is not a pane", () => {
    for (const overflow of ["visible", "hidden", "clip"]) {
      expect(keepsWheel(box({ y: { overflow } }), DOWN)).toBe(false);
    }
  });

  test("a pane that can move in the wheel's direction keeps it", () => {
    const cases: [number, typeof DOWN][] = [[100, DOWN], [100, UP], [0, DOWN], [2700, UP]];
    for (const [at, gesture] of cases) {
      expect(keepsWheel(box({ y: { at } }), gesture)).toBe(true);
    }
  });

  test("at an edge, a wheel pushing past it goes to the page", () => {
    expect(keepsWheel(box({ y: { at: 0 } }), UP)).toBe(false);
    expect(keepsWheel(box({ y: { at: 2700 } }), DOWN)).toBe(false);
  });

  test("positions round the way Lenis rounds them, so a half-pixel from the edge IS the edge", () => {
    expect(keepsWheel(box({ y: { at: 2699.5 } }), DOWN)).toBe(false);
    expect(keepsWheel(box({ y: { at: 0.4 } }), UP)).toBe(false);
    expect(keepsWheel(box({ y: { at: 2699.4 } }), DOWN)).toBe(true);
  });

  test("overscroll-behavior other than auto holds the wheel at the edges too", () => {
    expect(keepsWheel(box({ y: { at: 2700, overscroll: "contain" } }), DOWN)).toBe(true);
    expect(keepsWheel(box({ y: { at: 0, overscroll: "none" } }), UP)).toBe(true);
    expect(keepsWheel(box({ y: { max: 0, overscroll: "contain" } }), DOWN)).toBe(false);
  });

  test("the dominant axis of the gesture is the one asked about", () => {
    expect(keepsWheel(box({ x: { overflow: "auto", max: 1600 } }), RIGHT)).toBe(true);
    expect(keepsWheel(box({ x: { overflow: "auto", max: 1600, at: 1600 } }), RIGHT)).toBe(false);
    expect(keepsWheel(box({ y: { at: 100 } }), RIGHT)).toBe(false);
  });
});

type FakeNode = {
  id?: string;
  style: Record<string, string>;
  reads: number;
  scrollLeft: number;
  scrollTop: number;
  scrollWidth: number;
  scrollHeight: number;
  clientWidth: number;
  clientHeight: number;
};

function node(overflowY: string, geometry: Partial<FakeNode> = {}): FakeNode {
  const n: FakeNode = {
    style: { overflowX: "visible", overflowY, overscrollBehaviorX: "auto", overscrollBehaviorY: "auto" },
    reads: 0,
    scrollLeft: 0,
    scrollTop: 0,
    scrollWidth: 400,
    scrollHeight: 3000,
    clientWidth: 400,
    clientHeight: 300,
    ...geometry,
  };
  for (const key of ["scrollTop", "scrollHeight", "clientHeight"] as const) {
    const value = n[key];
    Object.defineProperty(n, key, {
      get() {
        n.reads += 1;
        return value;
      },
    });
  }
  return n;
}

(globalThis as { getComputedStyle?: unknown }).getComputedStyle = (n: FakeNode) => n.style;

describe("readScrollBox", () => {
  test("reads geometry only when the computed overflow can scroll", () => {
    const pane = node("auto");
    const plain = node("visible");

    expect(readScrollBox(pane as unknown as Element).y).toEqual({ overflow: "auto", overscroll: "auto", at: 0, max: 2700 });
    expect(pane.reads).toBeGreaterThan(0);

    expect(readScrollBox(plain as unknown as Element).y.max).toBe(0);
    expect(plain.reads).toBe(0);
  });
});

describe("createNestedScrollGate", () => {
  const wheel = (deltaY: number) => ({ deltaX: 0, deltaY, event: { type: "wheel" } });

  test("answers prevent from the gesture virtualScroll just saw, live", () => {
    const gate = createNestedScrollGate();
    const pane = node("auto", { scrollTop: 0 }) as unknown as HTMLElement;

    gate.virtualScroll(wheel(120));
    expect(gate.prevent(pane)).toBe(true);
    gate.virtualScroll(wheel(-120));
    expect(gate.prevent(pane)).toBe(false);

    const short = node("auto", { scrollHeight: 150 }) as unknown as HTMLElement;
    gate.virtualScroll(wheel(120));
    expect(gate.prevent(short)).toBe(false);
  });

  test("touch parks nothing: the browser owns it and prevent stays false", () => {
    const gate = createNestedScrollGate();
    const pane = node("auto") as unknown as HTMLElement;

    gate.virtualScroll(wheel(120));
    gate.virtualScroll({ deltaX: 0, deltaY: 120, event: { type: "touchmove" } });
    expect(gate.prevent(pane)).toBe(false);
  });

  test("before any gesture prevent is false, and never reads the element", () => {
    const gate = createNestedScrollGate();
    const pane = node("auto");
    expect(gate.prevent(pane as unknown as HTMLElement)).toBe(false);
    expect(pane.reads).toBe(0);
  });

  test("wraps the caller's hooks: their prevent wins, their virtualScroll verdict passes through", () => {
    const seen: number[] = [];
    const gate = createNestedScrollGate({
      prevent: (n) => n.id === "sidebar",
      virtualScroll: (d) => {
        seen.push(d.deltaY);
        return d.deltaY > 500 ? false : undefined;
      },
    });
    const sidebar = node("visible", { id: "sidebar" }) as unknown as HTMLElement;

    expect(gate.virtualScroll(wheel(120))).toBe(true);
    expect(gate.virtualScroll(wheel(900))).toBe(false);
    expect(seen).toEqual([120, 900]);
    expect(gate.prevent(sidebar)).toBe(true);
  });
});
