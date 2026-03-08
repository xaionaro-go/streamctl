package action

import (
	"fmt"
	"testing"

	"github.com/xaionaro-go/serializable/registry"
)

func TestRegisteredNames(t *testing.T) {
	fmt.Printf("Registered name for StartStreamByProfileName: %s\n", registry.ToTypeName(&StartStreamByProfileName{}))
}
