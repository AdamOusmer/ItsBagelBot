// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

type pointsAmountKind uint8

const (
	pointsInvalid pointsAmountKind = iota
	pointsInteger
	pointsScaled
	pointsHalf
	pointsAll
)

type PointsAmount struct {
	kind     pointsAmountKind
	value    int64
	overflow bool
}

func ParsePointsAmount(raw string) PointsAmount {
	arg := strings.ToLower(strings.TrimSpace(raw))
	switch arg {
	case "half":
		return PointsAmount{kind: pointsHalf}
	case "all":
		return PointsAmount{kind: pointsAll}
	}
	if amount := pointDigits(arg).integer(); amount.Valid() {
		return amount
	}
	return parsePointsDecimal(arg)
}

func parsePointsDecimal(arg string) PointsAmount {
	digits, multiplier, kind := arg, int64(1), pointsInteger
	switch {
	case strings.HasSuffix(arg, "k"):
		digits, multiplier, kind = strings.TrimSuffix(arg, "k"), 1e3, pointsScaled
	case strings.HasSuffix(arg, "m"):
		digits, multiplier, kind = strings.TrimSuffix(arg, "m"), 1e6, pointsScaled
	}
	if !pointDigits(digits).isDecimal(kind == pointsScaled) {
		return PointsAmount{}
	}
	value, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return PointsAmount{}
	}
	scaled := math.Round(value * float64(multiplier))
	if scaled >= 1<<52 {
		return pointDigits(digits).largeAmount(multiplier, kind)
	}
	return PointsAmount{kind: kind, value: int64(scaled)}
}

type pointDigits string

func (s pointDigits) largeAmount(multiplier int64, kind pointsAmountKind) PointsAmount {
	scaled, _ := new(big.Rat).SetString(string(s))
	scaled.Mul(scaled, big.NewRat(multiplier, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	value := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	if !value.IsInt64() {
		return PointsAmount{kind: kind, value: math.MaxInt64, overflow: true}
	}
	return PointsAmount{kind: kind, value: value.Int64()}
}

func (s pointDigits) integer() PointsAmount {
	value, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return PointsAmount{}
	}
	return PointsAmount{kind: pointsInteger, value: value}
}

func (s pointDigits) isDecimal(allowDot bool) bool {
	digits, dots := 0, 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.' && allowDot:
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

func (a PointsAmount) Valid() bool { return a.kind != pointsInvalid }

func (a PointsAmount) relative() bool { return a.kind == pointsHalf || a.kind == pointsAll }

func (a PointsAmount) Resolve(balance int64) int64 {
	switch a.kind {
	case pointsHalf:
		return balance / 2
	case pointsAll:
		return balance
	default:
		return a.value
	}
}

func parseWagerAmount(raw string) PointsAmount {
	if arg, prefixed := strings.CutPrefix(strings.TrimSpace(raw), "@"); prefixed {
		return pointDigits(arg).integer()
	}
	return ParsePointsAmount(raw)
}

type wagerAmountOutcome uint8

const (
	wagerInvalid wagerAmountOutcome = iota
	wagerBelowMin
	wagerAboveMax
	wagerOverBalance
	wagerOK
)

func (a PointsAmount) wager(balance, floor, cap int64) (int64, wagerAmountOutcome) {
	value := a.Resolve(balance)
	switch {
	case a.relative():
		value = min(value, cap)
	case !a.validFixedWager():
		return 0, wagerInvalid
	}
	switch {
	case value < floor:
		return 0, wagerBelowMin
	case value > cap:
		return 0, wagerAboveMax
	case value > balance:
		return 0, wagerOverBalance
	default:
		return value, wagerOK
	}
}

func (a PointsAmount) validFixedWager() bool {
	return a.Valid() && !a.overflow && a.value > 0
}
