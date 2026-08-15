package main

import "testing"

func TestEncodeBaiduMD5MatchesCapturedValues(t *testing.T) {
	tests := map[string]string{
		"4e6ff05e39d763d062288e3c2798de36": "38f426b7cnc43db126bb9b51eb8343d3",
		"aeec9c4e038d046789e2dfaeab8b1031": "02ae41002n4751a1aaa8555600491241",
	}
	for plain, encoded := range tests {
		if got := encodeBaiduMD5(plain); got != encoded {
			t.Fatalf("encodeBaiduMD5(%q) = %q, want %q", plain, got, encoded)
		}
	}
}

func TestCalculateDataOffsetMatchesCapturedValue(t *testing.T) {
	got := calculateDataOffset("416237033", "38f426b7cnc43db126bb9b51eb8343d3", 1786779706, 741385398)
	if got != 426185739 {
		t.Fatalf("calculateDataOffset() = %d, want %d", got, 426185739)
	}
}
