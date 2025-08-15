package xstring

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func ToReadable(s string) string {
	plain := norm.NFKD.String(s)
	var b strings.Builder
	for _, r := range plain {
		// Remove symbols (unicode.Symbol), keep everything else (including foreign letters)
		if !unicode.IsSymbol(r) {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), " ,\t\n\r")
}
