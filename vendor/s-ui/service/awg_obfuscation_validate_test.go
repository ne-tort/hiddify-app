package service

import (
	"testing"

	"github.com/alireza0/s-ui/database/model"
)

func intp(v int) *int       { return &v }
func strp(v string) *string { return &v }

func TestValidateAwg20ObfuscationFields_okSample(t *testing.T) {
	row := model.AwgObfuscationProfile{
		GeneratorSpec: `{"useExtremeMax":false}`,
		Jc:            intp(6),
		Jmin:          intp(200),
		Jmax:          intp(600),
		S1:            intp(10),
		S2:            intp(20),
		S3:            intp(5),
		S4:            intp(16),
		H1:            strp("100000000-100050000"),
		H2:            strp("1200000000-1200050000"),
		H3:            strp("2400000000-2400050000"),
		H4:            strp("3600000000-3600050000"),
	}
	if err := ValidateAwg20ObfuscationFields(&row); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAwg20ObfuscationFields_S1plus56EqS2(t *testing.T) {
	row := model.AwgObfuscationProfile{
		S1: intp(10),
		S2: intp(66),
	}
	if err := ValidateAwg20ObfuscationFields(&row); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateAwg20ObfuscationFields_S4TooHighWithoutExtreme(t *testing.T) {
	row := model.AwgObfuscationProfile{
		S4:            intp(40),
		GeneratorSpec: `{"useExtremeMax":false}`,
	}
	if err := ValidateAwg20ObfuscationFields(&row); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateAwg20ObfuscationFields_S4ExtremeAllowsHigh(t *testing.T) {
	row := model.AwgObfuscationProfile{
		S4:            intp(64),
		GeneratorSpec: `{"useExtremeMax":true}`,
	}
	if err := ValidateAwg20ObfuscationFields(&row); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAwg20ObfuscationFields_HOverlap(t *testing.T) {
	row := model.AwgObfuscationProfile{
		H1: strp("100-200"),
		H2: strp("150-250"),
	}
	if err := ValidateAwg20ObfuscationFields(&row); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateAwg20ObfuscationFields_JmaxTooSmall(t *testing.T) {
	row := model.AwgObfuscationProfile{Jmax: intp(80)}
	if err := ValidateAwg20ObfuscationFields(&row); err == nil {
		t.Fatal("expected error")
	}
}
