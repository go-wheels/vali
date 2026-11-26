package vali

import (
	"regexp"
	"time"
)

var (
	cnMobileFormat = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
	cnStandardTime = time.FixedZone("CST", 8*60*60)
)

func isCNMobile(value any) bool {
	raw, ok := stringValue(value)
	return ok && cnMobileFormat.MatchString(raw)
}

func isCNIDCard(value any) bool {
	raw, ok := stringValue(value)
	if !ok || len(raw) != 18 || !isMainlandProvinceCode(raw[:2]) {
		return false
	}

	// GB 11643-1999 uses ISO 7064 MOD 11-2 for the final character.
	weights := [...]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	var sum int
	for i, weight := range weights {
		digit := raw[i]
		if digit < '0' || digit > '9' {
			return false
		}
		sum += int(digit-'0') * weight
	}
	check := raw[17]
	if check == 'x' {
		check = 'X'
	}
	if check != "10X98765432"[sum%11] {
		return false
	}

	birth, err := time.ParseInLocation("20060102", raw[6:14], cnStandardTime)
	return err == nil && birth.Year() > 0 && !birth.After(time.Now())
}

func isMainlandProvinceCode(code string) bool {
	switch code {
	case "11", "12", "13", "14", "15",
		"21", "22", "23",
		"31", "32", "33", "34", "35", "36", "37",
		"41", "42", "43", "44", "45", "46",
		"50", "51", "52", "53", "54",
		"61", "62", "63", "64", "65":
		return true
	default:
		return false
	}
}
