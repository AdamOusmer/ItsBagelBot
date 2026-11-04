// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CODE, MAX_LOGIN_TEMPLATE, MAX_TEMPLATE_LENGTH } from './limits';
import { allChars, runeCount, type CharTest, type Text } from './text';
import type { Issue } from './types';

type Field = string;
type Count = number;
type Limit = number;

interface Bounds {
  lo: Limit;
  hi: Limit;
  over: Text;
}

export class Chk {
  constructor(
    readonly out: Issue[],
    readonly path: Field = '',
    readonly trigger: Text = '',
    readonly depth: Count = 0,
    readonly perm: Count = -1,
  ) {}

  add(field: Field, code: Text): void {
    this.out.push({ path: joinPath(this.path, field), code });
  }

  sub(field: Field): Chk {
    return new Chk(this.out, joinPath(this.path, field), this.trigger, this.depth, this.perm);
  }

  elem(i: Count): Chk {
    return new Chk(this.out, `${this.path}[${i}]`, this.trigger, this.depth, this.perm);
  }

  nest(): Chk {
    return new Chk(this.out, this.path, this.trigger, this.depth + 1, this.perm);
  }

  bound(field: Field, n: Count, { lo, hi, over }: Bounds): boolean {
    if (n === 0 && lo > 0) this.add(field, CODE.required);
    else if (n < lo) this.add(field, CODE.tooShort);
    else if (n > hi) this.add(field, over);
    else return true;
    return false;
  }

  text(field: Field, s: Text, lo: Limit, hi: Limit): boolean {
    return this.bound(field, runeCount(s), { lo, hi, over: CODE.tooLong });
  }

  count(field: Field, n: Count, lo: Limit, hi: Limit): boolean {
    return this.bound(field, n, { lo, hi, over: CODE.tooMany });
  }

  template(field: Field, s: Text): boolean {
    return this.text(field, s, 1, MAX_TEMPLATE_LENGTH);
  }

  reason(field: Field, s: Text, lo: Limit): boolean {
    return this.text(field, s, lo, MAX_TEMPLATE_LENGTH);
  }

  login(field: Field, s: Text, lo: Limit): boolean {
    return this.text(field, s, lo, MAX_LOGIN_TEMPLATE);
  }

  inRange(field: Field, n: Count, lo: Limit, hi: Limit): void {
    if (n < lo || n > hi) this.add(field, CODE.outOfRange);
  }

  nonZero(field: Field, n: Count): void {
    if (n === 0) this.add(field, CODE.outOfRange);
  }

  oneOf(field: Field, s: Text, set: readonly Text[]): void {
    if (set.includes(s)) return;
    this.add(field, s === '' ? CODE.required : CODE.invalidEnum);
  }

  format(field: Field, s: Text, re: RegExp): void {
    if (s === '') this.add(field, CODE.required);
    else if (!re.test(s)) this.add(field, CODE.invalidFormat);
  }

  chars(field: Field, s: Text, ok: CharTest): void {
    if (!allChars(s, ok)) this.add(field, CODE.invalidFormat);
  }

  atLeastOne(ok: boolean): void {
    if (!ok) this.add('', CODE.required);
  }

  stringList(field: Field, list: Text[], hi: Limit, each: (field: Field, item: Text) => void): void {
    if (!this.count(field, list.length, 0, hi)) return;
    list.forEach((s, i) => each(`${field}[${i}]`, s));
  }
}

export function joinPath(base: Field, field: Field): Field {
  if (field === '') return base;
  if (base === '') return field;
  return `${base}.${field}`;
}
