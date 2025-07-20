package kick

import (
	"github.com/go-ng/xatomic"
)

func (k *Kick) GetClient() Client {
	return *xatomic.LoadPointer(&k.Client)
}

func (k *Kick) SetClient(client Client) {
	xatomic.StorePointer(&k.Client, &client)
}
