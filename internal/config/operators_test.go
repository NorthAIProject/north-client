package config

import "testing"

func TestParseOperators(t *testing.T) {
	ids, err := parseOperators(" 00000000-0000-0000-0000-000000000001, ,00000000-0000-0000-0000-000000000002 ")
	if err != nil || len(ids) != 2 || ids[1].String() != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	if ids, err = parseOperators(""); err != nil || len(ids) != 0 {
		t.Fatalf("empty = %v, %v", ids, err)
	}
	if _, err = parseOperators("not-a-uuid"); err == nil {
		t.Fatal("a malformed id was accepted")
	}
}
