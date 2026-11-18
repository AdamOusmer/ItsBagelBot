// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type RenderInner = (raws: string[]) => string[];

const LITERAL_BRACE = '';
const NEST_STOPS = /[:{}|]/g;
const BRACES = /[{}]/g;
const PAYLOAD_BREAKERS = /[}|]/g;
const COND_NAME = 'if';

interface Range {
  start: number;
  end: number;
}

export function unnest(template: string, render: RenderInner): string {
  const pass = new NestPass(template);
  while (pass.step(render));
  return pass.text;
}

class NestPass {
  private at = 0;

  constructor(public text: string) {}

  step(render: RenderInner): boolean {
    const ranges = this.ranges();
    if (ranges.length === 0) return false;
    const raws = ranges.map((range) => this.text.slice(range.start, range.end));
    const values = render(raws);
    let out = '';
    let from = 0;
    let changed = false;
    ranges.forEach((range, i) => {
      const value = values[i] === raws[i] ? raws[i] : values[i].replaceAll('{', LITERAL_BRACE).replace(PAYLOAD_BREAKERS, '');
      changed = changed || value !== raws[i];
      out += this.text.slice(from, range.start) + value;
      from = range.end;
    });
    if (changed) this.text = out + this.text.slice(from);
    return changed;
  }

  private ranges(): Range[] {
    const out: Range[] = [];
    let open = this.text.indexOf('{');
    while (open >= 0) {
      this.at = open + 1;
      const inner = this.nestable() ? this.outer() : null;
      if (inner === null) this.at = open + 1;
      else out.push(...inner);
      open = this.text.indexOf('{', this.at);
    }
    return out;
  }

  private find(pattern: RegExp): number {
    pattern.lastIndex = this.at;
    const match = pattern.exec(this.text);
    return match === null ? -1 : match.index;
  }

  private nestable(): boolean {
    const stop = this.find(NEST_STOPS);
    if (stop <= this.at || this.text[stop] !== ':') return false;
    return this.text.slice(this.at, stop).toLowerCase() !== COND_NAME;
  }

  private outer(): Range[] | null {
    const inner: Range[] = [];
    for (;;) {
      const next = this.find(BRACES);
      if (next < 0) return null;
      this.at = next + 1;
      if (this.text[next] === '}') return inner;
      const end = this.find(BRACES);
      if (end < 0 || this.text[end] !== '}') return null;
      inner.push({ start: next, end: end + 1 });
      this.at = end + 1;
    }
  }
}
