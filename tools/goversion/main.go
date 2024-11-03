package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/hashicorp/go-version"
)

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		panic(fmt.Errorf("expected two arguments"))
	}
	op := flag.Arg(0)
	requiredVersionString := flag.Arg(1)

	requiredVersion, err := version.NewVersion(requiredVersionString)
	if err != nil {
		panic(err)
	}

	currentVersionString := runtime.Version()[len("go"):]
	currentVersion, err := version.NewVersion(currentVersionString)
	if err != nil {
		panic(err)
	}

	satisfied := false
	switch op {
	case "ge":
		satisfied = currentVersion.GreaterThanOrEqual(requiredVersion)
	default:
		panic(fmt.Errorf("unexpected operator: '%s'", op))
	}

	if !satisfied {
		fmt.Printf("false\n")
		os.Exit(1)
	}
	fmt.Printf("true\n")
	os.Exit(0)
}
