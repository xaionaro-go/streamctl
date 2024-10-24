package secret

// String stores a string in an encrypted state in memory, so that
// you don't accidentally leak these secrets via logging or whatever.
type String = Any[string]
