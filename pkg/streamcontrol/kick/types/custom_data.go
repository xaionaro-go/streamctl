package kick

import (
	"github.com/xaionaro-go/streamctl/pkg/secret"
)

type CustomData struct {
	Key      secret.String
	URL      string
	IsMature bool
	Language string
}
