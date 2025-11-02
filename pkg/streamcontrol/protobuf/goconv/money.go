package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
)

func MoneyGo2GRPC(
	money streamcontrol.Money,
) *streamcontrol_grpc.Money {
	return &streamcontrol_grpc.Money{
		Currency: CurrencyGo2GRPC(money.Currency),
		Amount:   money.Amount,
	}
}

func MoneyGRPC2Go(
	money *streamcontrol_grpc.Money,
) streamcontrol.Money {
	return streamcontrol.Money{
		Currency: CurrencyGRPC2Go(money.GetCurrency()),
		Amount:   money.GetAmount(),
	}
}
