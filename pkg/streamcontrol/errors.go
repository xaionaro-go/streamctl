package streamcontrol

import "fmt"

type ErrInvalidStreamProfileType struct {
	Received StreamProfile
	Expected StreamProfile
}

var _ error = ErrInvalidStreamProfileType{}

func (e ErrInvalidStreamProfileType) Error() string {
	return fmt.Sprintf(
		"received an invalid stream profile type: expected:%T, received:%T",
		e.Expected,
		e.Received,
	)
}

type ErrNoStreamControllerForProfile struct {
	StreamProfile StreamProfile
}

func (e ErrNoStreamControllerForProfile) Error() string {
	return fmt.Sprintf("no StreamController found for profile %T", e.StreamProfile)
}

type ErrStreamIDEmpty struct{}

var _ error = ErrStreamIDEmpty{}

func (e ErrStreamIDEmpty) Error() string {
	return "stream ID is empty"
}
