package vali

import (
	"math/big"
	"reflect"
	"regexp"
	"strings"
)

var (
	decimalSyntax = regexp.MustCompile("^[+-]?(?:[0-9]+(?:\\.[0-9]*)?|\\.[0-9]+)(?:[eE][+-]?[0-9]+)?$")
	integerSyntax = regexp.MustCompile("^[+-]?[0-9]+$")
)

// decimal stores significant digits and the position of the decimal point.
// Magnitudes compare without expanding exponents or rounding large integers.
type decimal struct {
	sign   int
	digits string
	point  big.Int
}

func parseNumber(raw string) (decimal, bool) {
	if !decimalSyntax.MatchString(raw) {
		return decimal{}, false
	}
	number := decimal{sign: 1}
	if raw[0] == '-' {
		number.sign = -1
	}
	if raw[0] == '-' || raw[0] == '+' {
		raw = raw[1:]
	}
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		number.point.SetString(raw[index+1:], 10)
		raw = raw[:index]
	}
	whole, fraction, _ := strings.Cut(raw, ".")
	joined := whole + fraction
	significant := strings.TrimLeft(joined, "0")
	if significant == "" {
		return decimal{}, true
	}
	offset := len(whole) - (len(joined) - len(significant))
	number.point.Add(&number.point, big.NewInt(int64(offset)))
	number.digits = strings.TrimRight(significant, "0")
	return number, true
}

func (n decimal) compare(other decimal) int {
	if n.sign != other.sign {
		return n.sign - other.sign
	}
	if n.sign == 0 {
		return 0
	}
	if position := n.point.Cmp(&other.point); position != 0 {
		return n.sign * position
	}
	return n.sign * strings.Compare(n.digits, other.digits)
}

func numberValue(value any) (decimal, bool) {
	if reflect.ValueOf(value).Kind() == reflect.Bool {
		return decimal{}, false
	}
	raw, ok := scalarText(value)
	if !ok {
		return decimal{}, false
	}
	return parseNumber(raw)
}

func isInteger(value any) bool {
	switch reflect.ValueOf(value).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	case reflect.String:
		raw, _ := stringValue(value)
		return integerSyntax.MatchString(raw)
	case reflect.Float32, reflect.Float64:
		number, ok := numberValue(value)
		return ok && (number.sign == 0 || number.point.Cmp(big.NewInt(int64(len(number.digits)))) >= 0)
	default:
		return false
	}
}
