package streamd

import (
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
)

type OptionsAggregated struct {
	P2PSetupServer types.FuncSetupServer
	P2PSetupClient types.FuncSetupClient
	SubprocessIO   subprocessIO
	LogstashAddr   string
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

// OptionSubprocessIO supplies the IO writers and streamd executable path
// used when spawning internal worker subprocesses (chat listeners,
// translator). Required if the StreamD instance will reach
// StartExternalChatHandler or StartTranslatorSubprocess; subprocess spawn
// returns an error otherwise. Construct via NewSubprocessIO so writer
// nilness and execPath emptiness are validated at startup.
type OptionSubprocessIO subprocessIO

func (opt OptionSubprocessIO) apply(opts *OptionsAggregated) {
	opts.SubprocessIO = subprocessIO(opt)
}

// OptionLogstashAddr forwards the parent streamd's --logstash-addr to
// every internal worker subprocess (chat listeners, translator) so their
// logs ship to the same logstash sink as the parent's. Empty value
// disables logstash forwarding for children. The address is passed as
// argv to children, so it appears in `ps` — do not embed credentials.
type OptionLogstashAddr string

func (opt OptionLogstashAddr) apply(opts *OptionsAggregated) {
	opts.LogstashAddr = string(opt)
}
