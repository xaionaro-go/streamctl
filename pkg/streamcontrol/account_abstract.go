package streamcontrol

type AbstractAccount interface {
	AccountGeneric[StreamProfile]
	GetImplementation() AccountCommons
}

func ToAbstractAccount[ProfileType StreamProfile](
	c AccountGeneric[ProfileType],
) AbstractAccount {
	if c == nil {
		return nil
	}
	return &abstractAccountController[ProfileType]{c}
}
