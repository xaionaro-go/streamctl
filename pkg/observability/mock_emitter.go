package observability

import logger "github.com/facebookincubator/go-belt/tool/logger/types"

type mockEmitter struct {
	LastEntry *logger.Entry
}

var _ logger.Emitter = (*mockEmitter)(nil)

func (e *mockEmitter) Emit(entry *logger.Entry) {
	e.LastEntry = entry
}
func (e *mockEmitter) Flush() {}
