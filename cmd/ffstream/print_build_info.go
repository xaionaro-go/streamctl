package main

import (
	"context"
	"encoding/json"
	"io"
	"runtime/debug"

	"github.com/xaionaro-go/streamctl/pkg/buildvars"
)

type buildVars struct {
	Version   string `json:",omitempty"`
	GitCommit string `json:",omitempty"`
	BuildDate string `json:",omitempty"`
}

type buildInfo struct {
	BuildInfo *debug.BuildInfo `json:",omitempty"`
	BuildVars *buildVars       `json:",omitempty"`
}

func getBuildInfo() buildInfo {
	result := buildInfo{
		BuildVars: &buildVars{
			Version:   buildvars.Version,
			GitCommit: buildvars.GitCommit,
			BuildDate: buildvars.BuildDateString,
		},
	}
	if *result.BuildVars == (buildVars{}) {
		result.BuildVars = nil
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return result
	}

	result.BuildInfo = bi
	return result
}

func printBuildInfo(
	ctx context.Context,
	out io.Writer,
) {
	bi := getBuildInfo()
	enc := json.NewEncoder(out)
	enc.SetIndent("", " ")
	err := enc.Encode(bi)
	assertNoError(ctx, err)
}
