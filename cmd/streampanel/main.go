package main

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streampanel"
)

func main() {
	streampanel.New("/tmp/test.yaml").Loop(context.Background())

}
