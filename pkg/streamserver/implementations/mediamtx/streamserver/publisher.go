package streamserver

type PublisherClosedNotifier struct {
	closedChan chan struct{}
}

func newPublisherClosedNotifier() *PublisherClosedNotifier {
	return &PublisherClosedNotifier{
		closedChan: make(chan struct{}),
	}
}

func (n *PublisherClosedNotifier) Close() error {
	close(n.closedChan)
	return nil
}

func (n *PublisherClosedNotifier) ClosedChan() <-chan struct{} {
	return n.closedChan
}
