package idgen

import (
	"errors"
	"strings"
)

const (
	alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	base     = int64(len(alphabet))
)

// Encode converts a 64-bit integer to a Base62 string.
func Encode(number int64) string {
	if number == 0 {
		return string(alphabet[0])
	}

	var sb strings.Builder
	// Process digits by repeatedly dividing by 62 and mapping the remainder
	for number > 0 {
		rem := number % base
		sb.WriteByte(alphabet[rem])
		number = number / base
	}

	// Reverse the accumulated bytes as division extracts digits in reverse order
	bytes := []byte(sb.String())
	for i, j := 0, len(bytes)-1; i < j; i, j = i+1, j-1 {
		bytes[i], bytes[j] = bytes[j], bytes[i]
	}
	return string(bytes)
}

// Decode converts a Base62 string back to a 64-bit integer.
func Decode(code string) (int64, error) {
	var number int64
	// Reconstruct the 64-bit integer by multiplying by 62 and adding index values
	for i := 0; i < len(code); i++ {
		pos := strings.IndexByte(alphabet, code[i])
		if pos == -1 {
			return 0, errors.New("invalid character in base62 string")
		}
		number = number*base + int64(pos)
	}
	return number, nil
}
