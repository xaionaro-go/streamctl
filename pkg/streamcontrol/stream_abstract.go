package streamcontrol

type AbstractStream interface {
	StreamGeneric[StreamProfile]
}

func ToAbstractStream[ProfileType StreamProfile](
	c StreamGeneric[ProfileType],
) AbstractStream {
	if c == nil {
		return nil
	}
	return &abstractStreamController[ProfileType]{c}
}
