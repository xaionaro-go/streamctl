package streamcontrol

type Message struct {
	Content   string
	Format    TextFormatType
	InReplyTo *EventID
}
