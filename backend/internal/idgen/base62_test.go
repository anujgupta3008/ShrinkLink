package idgen

import (
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	testCases := []struct {
		id   int64
		code string
	}{
		{0, "0"},
		{1, "1"},
		{61, "Z"},
		{62, "10"},
		{1024, "gw"},
		{999999, "4c91"},
		{1234567890, "1ly7vk"},
	}

	for _, tc := range testCases {
		encoded := Encode(tc.id)
		if encoded != tc.code {
			t.Errorf("Encode(%d) = %s; want %s", tc.id, encoded, tc.code)
		}

		decoded, err := Decode(tc.code)
		if err != nil {
			t.Errorf("Decode(%s) returned error: %v", tc.code, err)
		}
		if decoded != tc.id {
			t.Errorf("Decode(%s) = %d; want %d", tc.code, decoded, tc.id)
		}
	}
}

func TestDecodeInvalidChars(t *testing.T) {
	invalidCodes := []string{
		"a-b",
		"12_3",
		"abc$",
		" ",
	}

	for _, code := range invalidCodes {
		_, err := Decode(code)
		if err == nil {
			t.Errorf("Decode(%s) should have failed with invalid character error", code)
		}
	}
}
