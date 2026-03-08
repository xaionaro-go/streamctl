package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xaionaro-go/streamctl/tools/upgradeconfig"
)

func main() {
	inputPath := flag.String("input", "config.yaml", "path to the config file to upgrade")
	outputPath := flag.String("output", "config.yaml.new", "path to save the upgraded config")
	inplace := flag.Bool("inplace", false, "overwrite the input file")
	flag.Parse()

	if *inplace {
		*outputPath = *inputPath
	}

	data, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file: %v\n", err)
		os.Exit(1)
	}

	converted, err := upgradeconfig.ConvertConfigBytes(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting config: %v\n", err)
		os.Exit(1)
	}

	if string(data) == string(converted) {
		fmt.Println("Config is already up to date.")
		return
	}

	err = os.WriteFile(*outputPath, converted, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config upgraded and saved to %s\n", *outputPath)
}
