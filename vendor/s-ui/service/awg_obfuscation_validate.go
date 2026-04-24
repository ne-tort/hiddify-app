package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database/model"
)

type awgGeneratorSpecFlags struct {
	UseExtremeMax bool `json:"useExtremeMax"`
}

func parseUseExtremeMaxFromSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	var f awgGeneratorSpecFlags
	if err := json.Unmarshal([]byte(spec), &f); err != nil {
		return false
	}
	return f.UseExtremeMax
}

func parseDecimalRange(s string) (lo, hi uint64, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("empty h range")
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("h range must be lo-hi")
	}
	lo, err = strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("h range lo: %w", err)
	}
	hi, err = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("h range hi: %w", err)
	}
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo, hi, nil
}

func rangesOverlap(a0, a1, b0, b1 uint64) bool {
	return a0 <= b1 && b0 <= a1
}

// ValidateAwg20ObfuscationFields enforces AmneziaWG-Architect / README-style rules for AWG 2.0 fields.
// Call with the row about to be persisted (ints and strings as stored).
func ValidateAwg20ObfuscationFields(row *model.AwgObfuscationProfile) error {
	if row == nil {
		return nil
	}
	useExtreme := parseUseExtremeMaxFromSpec(row.GeneratorSpec)

	if row.S1 != nil && row.S2 != nil {
		if *row.S1+56 == *row.S2 {
			return fmt.Errorf("S1+56 must differ from S2 (got S1=%d S2=%d)", *row.S1, *row.S2)
		}
	}
	if row.S1 != nil && row.S3 != nil {
		if *row.S3 == *row.S1+56 {
			return fmt.Errorf("S3 must differ from S1+56")
		}
	}
	if row.S2 != nil && row.S3 != nil {
		if *row.S3 == *row.S2+92 {
			return fmt.Errorf("S3 must differ from S2+92")
		}
	}

	if row.S4 != nil {
		maxS4 := 32
		if useExtreme {
			maxS4 = 128
		}
		if *row.S4 < 1 || *row.S4 > maxS4 {
			return fmt.Errorf("S4 must be in 1..%d (extreme=%v)", maxS4, useExtreme)
		}
	}

	if row.Jc != nil && (*row.Jc < 1 || *row.Jc > 128) {
		return fmt.Errorf("Jc must be in 1..128")
	}
	if row.Jmax != nil && *row.Jmax <= 81 {
		return fmt.Errorf("Jmax must be > 81")
	}
	if row.Jmin != nil && row.Jmax != nil && *row.Jmax <= *row.Jmin+64 {
		return fmt.Errorf("Jmax must be > Jmin+64")
	}

	var hr [4]struct {
		lo, hi uint64
		ok     bool
	}
	strs := []*string{row.H1, row.H2, row.H3, row.H4}
	for i, sp := range strs {
		if sp == nil || strings.TrimSpace(*sp) == "" {
			continue
		}
		lo, hi, err := parseDecimalRange(*sp)
		if err != nil {
			return fmt.Errorf("H%d: %w", i+1, err)
		}
		hr[i].lo, hr[i].hi, hr[i].ok = lo, hi, true
	}
	for i := 0; i < 4; i++ {
		if !hr[i].ok {
			continue
		}
		for j := i + 1; j < 4; j++ {
			if !hr[j].ok {
				continue
			}
			if rangesOverlap(hr[i].lo, hr[i].hi, hr[j].lo, hr[j].hi) {
				return fmt.Errorf("H%d and H%d ranges overlap", i+1, j+1)
			}
		}
	}
	return nil
}
