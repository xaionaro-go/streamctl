package mainprocess

import (
	"crypto/sha1"
	"fmt"
)

func checkPassword(
	a, b string,
) error {
	// naive mostly-timing-attack-resistant comparison algo

	h0 := sha1.Sum([]byte(a))
	h1 := sha1.Sum([]byte(b))

	match := true
	for idx := range h0 {
		charMatches := h0[idx] == h1[idx]
		match = match && charMatches
	}

	if !match {
		return fmt.Errorf("the password does not match")
	}

	return nil
}
