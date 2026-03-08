package kick

func (k *Kick) GetClient() Client {
	return k.Client
}

func (k *Kick) SetClient(client Client) {
	k.Client = client
}
