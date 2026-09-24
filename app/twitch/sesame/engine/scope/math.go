// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"math"
	"strconv"
)

const MaxMathExpr = 64

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

type mathParser struct {
	src string
	pos int
}

type mathInt int64

func (p *mathParser) peek() byte {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *mathParser) atEnd() bool { return p.peek() == 0 }

func (p *mathParser) sum() (mathInt, bool) {
	return p.binary("+-", (*mathParser).product, applyAdditive)
}

func (p *mathParser) product() (mathInt, bool) {
	return p.binary("*/", (*mathParser).unary, applyMultiplicative)
}

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

func containsByte(ops string, c byte) bool {
	for i := 0; i < len(ops); i++ {
		if ops[i] == c {
			return true
		}
	}
	return false
}

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

func (p *mathParser) number() (mathInt, bool) {
	p.peek()
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

func applyMultiplicative(op byte, a, b mathInt) (mathInt, bool) {
	if op == '*' {
		return mulInt(a, b)
	}
	return divInt(a, b)
}

func addInt(a, b mathInt) (mathInt, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}

func mulInt(a, b mathInt) (mathInt, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a == math.MinInt64 && b == -1 {
		return 0, false
	}
	c := a * b
	if c/b != a {
		return 0, false
	}
	return c, true
}

func divInt(a, b mathInt) (mathInt, bool) {
	if b == 0 || quotientOverflows(a, b) {
		return 0, false
	}
	return a / b, true
}

func quotientOverflows(a, b mathInt) bool {
	return a == math.MinInt64 && b == -1
}

func negate(a mathInt) (mathInt, bool) {
	if a == math.MinInt64 {
		return 0, false
	}
	return -a, true
}
