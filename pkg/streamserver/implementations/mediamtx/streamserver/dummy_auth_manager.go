package streamserver

import (
	"github.com/xaionaro-go/mediamtx/pkg/auth"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
)

type dummyAuthManager struct{}

var _ pathmanager.AuthManager = (*dummyAuthManager)(nil)

func (m *dummyAuthManager) Authenticate(req *auth.Request) error {
	return nil
}
