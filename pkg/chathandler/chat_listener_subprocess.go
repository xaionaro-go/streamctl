package chathandler

// Flag constants for chat listener subprocess arguments.
// Used by StartExternalChatHandler (pkg/streamd) to build command args
// and by pkg/chathandler/process to parse them.
const (
	FlagChatListenerMode        = "internal-chat-listener"
	FlagChatListenerPlatform    = "internal-chat-listener-platform"
	FlagChatListenerType        = "internal-chat-listener-type"
	FlagChatListenerStreamdAddr = "internal-chat-listener-streamd-addr"
)
