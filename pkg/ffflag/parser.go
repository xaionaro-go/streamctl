package ffflag

import (
	"fmt"
	"strings"
)

type Parser struct {
	Options                 []*GenericOption
	CollectedUnknownOptions []string
	CollectedNonFlags       []string

	nextCollectorOfUnknownOptions []string
}

func NewParser() *Parser {
	return &Parser{}
}

func AddParameter[V any, W Wrapped[V]](
	p *Parser,
	name string,
	collectUnknownFlags bool,
	v W,
) *Option[V, W] {
	opt := &GenericOption{
		OptionSettings: OptionSettings{
			Name:                  name,
			WithArgument:          true,
			CollectUnknownOptions: collectUnknownFlags,
		},
		Wrapped: abstractWrapper[V]{v},
	}
	p.Options = append(p.Options, opt)
	return &Option[V, W]{GenericOption: opt}
}

func AddFlag(
	p *Parser,
	name string,
	collectUnknownFlags bool,
) *Option[bool, *Bool] {
	v := ptr(Bool(false))
	opt := &GenericOption{
		OptionSettings: OptionSettings{
			Name:                  name,
			WithArgument:          false,
			CollectUnknownOptions: collectUnknownFlags,
		},
		Wrapped: abstractWrapper[bool]{v},
	}
	p.Options = append(p.Options, opt)
	return &Option[bool, *Bool]{GenericOption: opt}
}

func NewDefaultParser() *Parser {
	p := NewParser()
	AddParameter(p, "i", true, ptr(StringsAsSeparateFlags(nil)))
	return p
}

func (p *Parser) Parse(args []string) error {
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			p.nextCollectorOfUnknownOptions = append(p.nextCollectorOfUnknownOptions, arg)
			continue
		}

		if arg == "--" {
			p.CollectedNonFlags = args[idx+1:]
			break
		}

		flag := p.findOptionByName(arg[1:])
		if flag == nil {
			p.nextCollectorOfUnknownOptions = append(p.nextCollectorOfUnknownOptions, arg[1:])
			continue
		}

		var value string
		if flag.WithArgument {
			if idx+1 >= len(args) {
				return fmt.Errorf("the flag '%s' (raw: '%s') at position #%d requires an argument, but one is not provided", flag.Name, arg, idx)
			}
			idx++
			value = args[idx]
		}

		err := flag.Parse(value)
		if err != nil {
			return fmt.Errorf("unable to parse the argument to the flag '%s' (raw: '%s') at position #%d: %w", flag.Name, arg, idx, err)
		}

		if flag.CollectUnknownOptions {
			flag.CollectedUnknownOptions = append(
				flag.CollectedUnknownOptions,
				p.nextCollectorOfUnknownOptions,
			)
			p.nextCollectorOfUnknownOptions = nil
		}
	}

	p.CollectedUnknownOptions = p.nextCollectorOfUnknownOptions
	return nil
}

func (p *Parser) findOptionByName(flagName string) *GenericOption {
	for _, opt := range p.Options {
		if opt.Name == "" {
			continue
		}
		if flagName == opt.Name {
			return opt
		}
	}
	return nil
}
