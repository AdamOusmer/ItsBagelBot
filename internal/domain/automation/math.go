// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"
)

var ErrMath = errors.New("math_error")

type MathAction struct {
	Expression string `json:"expression"`
	SaveAs     string `json:"save_as"`
}

func checkMath(c chk, p MathAction) {
	c.template("expression", p.Expression)
	c.variableName("save_as", p.SaveAs)
}

type mathFunc struct {
	minArgs int
	maxArgs int
	apply   func(args []float64, random func() float64) float64
}

const anyArgs = MaxMathExpressionRunes

func unaryMath(fn func(float64) float64) mathFunc {
	return mathFunc{minArgs: 1, maxArgs: 1, apply: func(args []float64, _ func() float64) float64 { return fn(args[0]) }}
}

var mathFuncs = map[string]mathFunc{
	"round":  unaryMath(math.Round),
	"floor":  unaryMath(math.Floor),
	"ceil":   unaryMath(math.Ceil),
	"abs":    unaryMath(math.Abs),
	"min":    {minArgs: 1, maxArgs: anyArgs, apply: func(args []float64, _ func() float64) float64 { return slices.Min(args) }},
	"max":    {minArgs: 1, maxArgs: anyArgs, apply: func(args []float64, _ func() float64) float64 { return slices.Max(args) }},
	"random": {minArgs: 2, maxArgs: 2, apply: mathRandom},
}

func mathRandom(args []float64, random func() float64) float64 {
	lo, hi := min(args[0], args[1]), max(args[0], args[1])
	if lo == math.Trunc(lo) && hi == math.Trunc(hi) {
		return min(lo+math.Floor(random()*(hi-lo+1)), hi)
	}
	return lo + random()*(hi-lo)
}

// EvalMath evaluates an already rendered expression. random returns a value in [0, 1).
func EvalMath(expression string, random func() float64) (string, error) {
	if utf8.RuneCountInString(expression) > MaxMathExpressionRunes {
		return "", ErrMath
	}
	p := &mathParser{src: expression, random: random}
	value := p.sum()
	if p.failed || p.peek() != 0 {
		return "", ErrMath
	}
	return formatMath(value)
}

func formatMath(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", ErrMath
	}
	if value == 0 {
		return "0", nil
	}
	return strconv.FormatFloat(value, 'f', -1, 64), nil
}

// The expression is at most MaxMathExpressionRunes, which bounds recursion depth.
type mathParser struct {
	src    string
	pos    int
	failed bool
	random func() float64
}

func (p *mathParser) fail() float64 {
	p.failed = true
	p.pos = len(p.src)
	return 0
}

func (p *mathParser) peek() byte {
	for p.pos < len(p.src) && isMathSpace(p.src[p.pos]) {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func isMathSpace(b byte) bool { return b == ' ' || b == '\t' }

func (p *mathParser) sum() float64 {
	left := p.product()
	for {
		switch p.peek() {
		case '+':
			p.pos++
			left += p.product()
		case '-':
			p.pos++
			left -= p.product()
		default:
			return left
		}
	}
}

func (p *mathParser) product() float64 {
	left := p.unary()
	for {
		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			return left
		}
		p.pos++
		left = p.multiply(op, left, p.unary())
	}
}

func (p *mathParser) multiply(op byte, left, right float64) float64 {
	if op == '*' {
		return left * right
	}
	if right == 0 {
		return p.fail()
	}
	if op == '/' {
		return left / right
	}
	return math.Mod(left, right)
}

func (p *mathParser) unary() float64 {
	if p.peek() == '-' {
		p.pos++
		return -p.unary()
	}
	return p.power()
}

func (p *mathParser) power() float64 {
	base := p.primary()
	if p.peek() != '^' {
		return base
	}
	p.pos++
	return math.Pow(base, p.unary())
}

func (p *mathParser) primary() float64 {
	b := p.peek()
	switch {
	case b == '(':
		return p.group()
	case isMathDigit(b):
		return p.number()
	case isMathLetter(b):
		return p.call()
	}
	return p.fail()
}

func (p *mathParser) group() float64 {
	p.pos++
	value := p.sum()
	if !p.expect(')') {
		return p.fail()
	}
	return value
}

func (p *mathParser) expect(want byte) bool {
	if p.peek() != want {
		return false
	}
	p.pos++
	return true
}

func isMathDigit(b byte) bool { return (b >= '0' && b <= '9') || b == '.' }

func isMathLetter(b byte) bool { return b >= 'a' && b <= 'z' }

func (p *mathParser) number() float64 {
	start := p.pos
	for p.pos < len(p.src) && isMathDigit(p.src[p.pos]) {
		p.pos++
	}
	value, err := strconv.ParseFloat(p.src[start:p.pos], 64)
	if err != nil {
		return p.fail()
	}
	return value
}

func (p *mathParser) call() float64 {
	start := p.pos
	for p.pos < len(p.src) && isMathLetter(p.src[p.pos]) {
		p.pos++
	}
	fn, known := mathFuncs[p.src[start:p.pos]]
	if !known || !p.expect('(') {
		return p.fail()
	}
	args := p.arguments()
	if p.failed || len(args) < fn.minArgs || len(args) > fn.maxArgs {
		return p.fail()
	}
	return fn.apply(args, p.random)
}

func (p *mathParser) arguments() []float64 {
	args := []float64{p.sum()}
	for p.expect(',') {
		args = append(args, p.sum())
	}
	if !p.expect(')') {
		p.fail()
	}
	return args
}
