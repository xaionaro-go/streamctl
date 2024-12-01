package streamd

import (
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
)

type OptionsAggregated struct {
	P2PSetupServer types.FuncSetupServer
	P2PSetupClient types.FuncSetupClient
}

type Option interface {
	apply(*OptionsAggregated)
}

type Options []Option

func (s Options) apply(opts *OptionsAggregated) {
	for _, opt := range s {
		opt.apply(opts)
	}
}

func (s Options) Aggregate() OptionsAggregated {
	opts := OptionsAggregated{}
	s.apply(&opts)
	return opts
}

type OptionP2PSetupServer types.FuncSetupServer

func (opt OptionP2PSetupServer) apply(opts *OptionsAggregated) {
	opts.P2PSetupServer = types.FuncSetupServer(opt)
}

type OptionP2PSetupClient types.FuncSetupClient

func (opt OptionP2PSetupClient) apply(opts *OptionsAggregated) {
	opts.P2PSetupClient = types.FuncSetupClient(opt)
}
