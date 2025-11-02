package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
)

func CurrencyGo2GRPC(
	currency streamcontrol.Currency,
) streamcontrol_grpc.Currency {
	switch currency {
	case streamcontrol.UndefinedCurrency:
		return streamcontrol_grpc.Currency_CURRENCY_UNDEFINED
	case streamcontrol.CurrencyUSD:
		return streamcontrol_grpc.Currency_CURRENCY_USD
	case streamcontrol.CurrencyEUR:
		return streamcontrol_grpc.Currency_CURRENCY_EUR
	case streamcontrol.CurrencyGBP:
		return streamcontrol_grpc.Currency_CURRENCY_GBP
	case streamcontrol.CurrencyJPY:
		return streamcontrol_grpc.Currency_CURRENCY_JPY
	case streamcontrol.CurrencyTwitchBits:
		return streamcontrol_grpc.Currency_CURRENCY_TWITCH_BITS
	}
	return streamcontrol_grpc.Currency_CURRENCY_OTHER
}

func CurrencyGRPC2Go(
	currency streamcontrol_grpc.Currency,
) streamcontrol.Currency {
	switch currency {
	case streamcontrol_grpc.Currency_CURRENCY_UNDEFINED:
		return streamcontrol.UndefinedCurrency
	case streamcontrol_grpc.Currency_CURRENCY_USD:
		return streamcontrol.CurrencyUSD
	case streamcontrol_grpc.Currency_CURRENCY_EUR:
		return streamcontrol.CurrencyEUR
	case streamcontrol_grpc.Currency_CURRENCY_GBP:
		return streamcontrol.CurrencyGBP
	case streamcontrol_grpc.Currency_CURRENCY_JPY:
		return streamcontrol.CurrencyJPY
	case streamcontrol_grpc.Currency_CURRENCY_TWITCH_BITS:
		return streamcontrol.CurrencyTwitchBits
	}
	return streamcontrol.CurrencyOther
}
