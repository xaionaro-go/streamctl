package xstring

import (
	"testing"
)

func TestToReadable(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"Hello, world!", "Hello, world!"},
		{"Café", "Café"},
		{"e\u0301", "é"},
		{"Go语言", "Go语言"},
		{"𝔘𝔫𝔦𝔠𝔬𝔡𝔢", "Unicode"},
		{"🥛 , 𝑚𝑖𝐼𝐊𝐞𝐔!+", "miIKeU!"},
		{"𝕱𝖗𝖆𝖓ç𝖔𝖎𝖘𝖊 & Dᵢₑ𝘴ₑ 🇫🇷", "Françoise & Diese"},
	}

	for _, tt := range tests {
		result := ToReadable(tt.input)
		if result != tt.expected {
			t.Errorf("ToReadable(%q) = %q; want %q", tt.input, result, tt.expected)
		}
	}
}
