package streamcontrol

import "fmt"

type ErrInvalidStreamProfileType struct {
	Received AbstractStreamProfile
	Expected AbstractStreamProfile
}

var _ error = ErrInvalidStreamProfileType{}

func (e ErrInvalidStreamProfileType) Error() string {
	return fmt.Sprintf("received an invalid stream profile type: expected:%T, received:%T", e.Expected, e.Received)
}

type ErrNoStreamControllerForProfile struct {
	StreamProfile AbstractStreamProfile
}

func (e ErrNoStreamControllerForProfile) Error() string {
	return fmt.Sprintf("no StreamController found for profile %T", e.StreamProfile)
}
