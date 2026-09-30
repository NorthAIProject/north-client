package util

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
)

func GetTripKey(passengerID, direction string) string {
	return fmt.Sprintf("%s%s", passengerID, direction)
}

func DurationToString(d time.Duration) string {
	return fmt.Sprintf("%02d", int(math.Floor(d.Minutes()/60))) + ":" + fmt.Sprintf("%02d", int(math.Mod(d.Minutes(), 60)))
}

func ToUpper(ss []string) []string {
	return Map(ss, func(s string) string {
		return strings.ToUpper(s)
	})
}

func MatchesWildCard(pattern, input string) bool {
	patternReader := strings.NewReader(pattern)
	inputReader := strings.NewReader(input)
	for {
		pr, _, pErr := patternReader.ReadRune()
		ir, _, iErr := inputReader.ReadRune()

		if pErr == io.EOF && iErr == io.EOF {
			// same length and all runes to this point are equal
			return true
		}
		if pErr != nil || iErr != nil {
			return false
		}

		if pr == '?' {
			continue
		}
		if unicode.ToLower(pr) != unicode.ToLower(ir) {
			return false
		}
	}
}
