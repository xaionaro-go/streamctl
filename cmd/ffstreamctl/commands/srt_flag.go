package commands

import (
	"fmt"

	"github.com/xaionaro-go/libsrt/sockopt"
)

func srtFlagNameToID(
	name string,
) (sockopt.Sockopt, error) {
	sockOpt, ok := sockopt.FromString(name)
	if !ok {
		return sockopt.Sockopt(0), fmt.Errorf("unknown socket option '%s'", name)
	}

	if !sockOpt.DONOTUSE_IsIntOpt() {
		return sockopt.Sockopt(0), fmt.Errorf("socket option '%s' is not an integer-value option", name)
	}

	return sockOpt, nil
}
