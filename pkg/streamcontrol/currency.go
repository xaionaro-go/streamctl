package streamcontrol

import "fmt"

type Currency int

const (
	UndefinedCurrency = Currency(iota)
	CurrencyUSD
	CurrencyEUR
	CurrencyGBP
	CurrencyJPY
	CurrencyTwitchBits
	CurrencyOther
	endOfCurrency
)

func (c Currency) String() string {
	switch c {
	case UndefinedCurrency:
		return "undefined"
	case CurrencyUSD:
		return "usd"
	case CurrencyEUR:
		return "eur"
	case CurrencyGBP:
		return "gbp"
	case CurrencyJPY:
		return "jpy"
	case CurrencyTwitchBits:
		return "twitch_bits"
	case CurrencyOther:
		return "other"
	default:
		return fmt.Sprintf("unknown_%d", int(c))
	}
}

func ParseCurrency(s string) Currency {
	for c := UndefinedCurrency; c < endOfCurrency; c++ {
		if c.String() == s {
			return c
		}
	}
	return UndefinedCurrency
}
