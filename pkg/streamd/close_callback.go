package streamd

type closeCallback struct {
	Callback func() error
	Name     string
}
