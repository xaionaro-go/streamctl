package types

type Stream interface {
	Drain() error
	Close() error
}
