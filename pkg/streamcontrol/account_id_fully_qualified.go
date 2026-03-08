package streamcontrol

import "fmt"

type AccountIDFullyQualified struct {
	PlatformID PlatformID // can never be empty
	AccountID  AccountID  // can never be empty
}

func (id AccountIDFullyQualified) Validate() error {
	if id.PlatformID == "" {
		return fmt.Errorf("platform ID is empty")
	}
	if id.AccountID == "" {
		return fmt.Errorf("account ID is empty")
	}
	return nil
}

func NewAccountIDFullyQualified(platID PlatformID, accID AccountID) AccountIDFullyQualified {
	id := AccountIDFullyQualified{
		PlatformID: platID,
		AccountID:  accID,
	}
	if err := id.Validate(); err != nil {
		panic(err)
	}
	return id
}
