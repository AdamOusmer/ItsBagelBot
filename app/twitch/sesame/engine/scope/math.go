// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"math"
	"strconv"
)

// MaxMathExpr caps the bytes {math:…} will look at.
//
// Decision record. The evaluator below is a recursive-descent parser, so an
// expression's nesting depth is stack depth, and its cost is linear in the
// input either way — the cap is not there to bound CPU, it is there to bound
// the STACK a broadcaster's template can ask for. 64 bytes of '(' is 64
// frames, which is nothing; 64 KB of '(' would be a crash in a goroutine that
// is holding a chat line.
//
//   - no cap: rejected. The template is broadcaster-authored (not viewer
//     input), but "authored by a trusted person" is not "small", and a
//     paste accident should not be able to reach the stack limit.
//   - a depth counter instead of a length cap: rejected as more machinery for
//     the same guarantee — length already bounds depth, since every level of
//     nesting costs at least one byte.
//   - 64: the longest expression in the imported corpora (Nightbot $(eval …)
//     bodies that are pure arithmetic) is 21 bytes, so this is three times
//     the widest thing anyone has actually written and still visibly a cap.
const MaxMathExpr = 64

// evalMath evaluates a small integer arithmetic expression and renders the
// result, or returns "" for anything it will not evaluate: an expression over
// MaxMathExpr bytes, a character outside "0-9 + - * / ( )" and space, an
// unbalanced paren, a division by zero, or an int64 overflow at any step.
//
// Every rejection is the SAME empty answer on purpose. A token that resolved
// to nothing renders its fallback ({math:1/0|—}), which is the one behaviour
// a broadcaster can write a sensible template against; distinguishing "bad
// syntax" from "divided by zero" in chat would mean printing an error message
// into a stream's chat, which no one wants.
//
// It is hand-written rather than an expression library because the grammar is
// four operators over integers: a library would add a dependency, a much
// larger surface (variables, functions, floats, string coercion) than the
// pinned grammar, and no way to promise the TypeScript preview in
// console/shared/lib/pure.ts computes the identical answer. The two are
// pinned against one another by testdata/pure.golden.json.
func evalMath(expr string) string {
	if len(expr) > MaxMathExpr {
		return ""
	}
	p := &mathParser{src: expr}
	val, ok := p.sum()
	if !ok || !p.atEnd() {
		return ""
	}
	return strconv.FormatInt(int64(val), 10)
}

// mathParser is a recursive-descent parser over the expression bytes:
//
//	sum     := product (('+' | '-') product)*
//	product := unary (('*' | '/') unary)*
//	unary   := '-' unary | primary
//	primary := digits | '(' sum ')'
//
// Precedence and associativity fall out of the nesting, which is why this
// shape was chosen over a shunting-yard pass: the same four rules are what
// the TypeScript port has to mirror, and a grammar is easier to mirror than
// an operator table plus a stack discipline.
type mathParser struct {
	src string
	pos int
}

// mathInt is the integer domain a {math:…} expression evaluates in: signed
// 64-bit, where every fold below refuses rather than wraps.
//
// It is a defined type rather than a plain int64 so the checked folds cannot
// be handed a number that did not come out of this parser: an int64 read from
// a viewer count or a counter carries no overflow contract, and folding one
// through here unchecked is how a wrapped negative ends up printed in chat as
// if it were an answer.
type mathInt int64

// peek returns the next non-space byte without consuming it, or 0 at the end.
func (p *mathParser) peek() byte {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// atEnd reports whether only spaces remain. A parse that stops early (say
// "1+2)" or "1 2") is a failed parse, not a prefix result.
func (p *mathParser) atEnd() bool { return p.peek() == 0 }

// sum parses the '+' / '-' level, left-associative.
func (p *mathParser) sum() (mathInt, bool) {
	return p.binary("+-", (*mathParser).product, applyAdditive)
}

// product parses the '*' / '/' level, left-associative.
func (p *mathParser) product() (mathInt, bool) {
	return p.binary("*/", (*mathParser).unary, applyMultiplicative)
}

// binary is the one left-associative loop both levels are: parse an operand,
// then fold each following operator drawn from ops. Sharing it keeps the two
// levels from drifting and keeps each of them to a single line.
func (p *mathParser) binary(
	ops string,
	operand func(*mathParser) (mathInt, bool),
	apply func(byte, mathInt, mathInt) (mathInt, bool),
) (mathInt, bool) {
	val, ok := operand(p)
	if !ok {
		return 0, false
	}
	for containsByte(ops, p.peek()) {
		op := p.src[p.pos]
		p.pos++
		rhs, rhsOK := operand(p)
		if !rhsOK {
			return 0, false
		}
		if val, ok = apply(op, val, rhs); !ok {
			return 0, false
		}
	}
	return val, true
}

// containsByte reports whether c is one of the operator bytes. A 0 byte (the
// end of the expression) is never in ops, so the fold loop terminates.
func containsByte(ops string, c byte) bool {
	for i := 0; i < len(ops); i++ {
		if ops[i] == c {
			return true
		}
	}
	return false
}

// unary parses a leading '-' run. A leading '+' is deliberately NOT accepted:
// "+2" is not an expression anyone writes, and refusing it keeps the grammar
// exactly the pinned "ints and + - * / ( )" rather than one spelling wider
// than the TypeScript port would have to guess at.
func (p *mathParser) unary() (mathInt, bool) {
	if p.peek() != '-' {
		return p.primary()
	}
	p.pos++
	val, ok := p.unary()
	if !ok {
		return 0, false
	}
	return negate(val)
}

// primary parses a parenthesized sub-expression or a run of digits.
func (p *mathParser) primary() (mathInt, bool) {
	if p.peek() != '(' {
		return p.number()
	}
	p.pos++
	val, ok := p.sum()
	if !ok || p.peek() != ')' {
		return 0, false
	}
	p.pos++
	return val, true
}

// number parses a run of decimal digits into an int64. A run that does not
// fit (or no digits at all) fails the parse rather than saturating: a
// silently clamped number is a wrong answer printed with confidence.
func (p *mathParser) number() (mathInt, bool) {
	p.peek() // skip leading spaces
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	val, err := strconv.ParseInt(p.src[start:p.pos], 10, 64)
	return mathInt(val), err == nil
}

// applyAdditive folds one '+' or '-', failing on int64 overflow.
func applyAdditive(op byte, a, b mathInt) (mathInt, bool) {
	if op == '-' {
		neg, ok := negate(b)
		if !ok {
			return 0, false
		}
		b = neg
	}
	return addInt(a, b)
}

// applyMultiplicative folds one '*' or '/', failing on overflow or division
// by zero.
func applyMultiplicative(op byte, a, b mathInt) (mathInt, bool) {
	if op == '*' {
		return mulInt(a, b)
	}
	return divInt(a, b)
}

// addInt adds with an overflow check rather than wrapping. Wrapping would
// print a large negative number for a large positive sum, which reads as a
// working feature that is lying.
func addInt(a, b mathInt) (mathInt, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}

// mulInt multiplies with an overflow check. The zero case is separate because
// the division-based check below cannot divide by it.
func mulInt(a, b mathInt) (mathInt, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	// The MinInt64 * -1 case is called out separately: its wrapped product is
	// MinInt64 again, so the division check below reads it as exact and would
	// return a negative answer for a positive product. Every other overflow
	// fails the c/b round trip.
	if a == math.MinInt64 && b == -1 {
		return 0, false
	}
	c := a * b
	if c/b != a {
		return 0, false
	}
	return c, true
}

// divInt truncates toward zero (Go's own integer division), refusing a zero
// divisor and the one overflowing quotient, MinInt64 / -1.
func divInt(a, b mathInt) (mathInt, bool) {
	if b == 0 || quotientOverflows(a, b) {
		return 0, false
	}
	return a / b, true
}

// quotientOverflows names the single quotient int64 cannot hold: MinInt64 / -1
// is 2^63, one past the top of the range.
func quotientOverflows(a, b mathInt) bool {
	return a == math.MinInt64 && b == -1
}

// negate flips a sign, refusing MinInt64 which has no positive counterpart.
func negate(a mathInt) (mathInt, bool) {
	if a == math.MinInt64 {
		return 0, false
	}
	return -a, true
}
