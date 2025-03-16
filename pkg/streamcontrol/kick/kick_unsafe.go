package kick

import (
	"github.com/go-ng/xatomic"
	"github.com/scorfly/gokick"
)

func (k *Kick) getClient() *gokick.Client {
	return xatomic.LoadPointer(&k.Client)
}

func (k *Kick) setClient(client *gokick.Client) {
	xatomic.StorePointer(&k.Client, client)
}
