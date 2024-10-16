//go:build android || android_test
// +build android android_test

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

func getSystemProperty(prop string) (string, error) {
	cmd := exec.Command("getprop", prop)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func init() {
	platformGetFlagsFuncs = append(platformGetFlagsFuncs, getFlagsAndroidFromSysprop)
	platformGetFlagsFuncs = append(platformGetFlagsFuncs, getFlagsAndroidFromFiles)
}

func getFlagsAndroidFromSysprop(flags *Flags) {
	ctx := context.TODO()
	sv := reflect.ValueOf(flags).Elem()
	st := reflect.TypeOf(flags).Elem()
	for i := 0; i < sv.NumField(); i++ {
		f := sv.Field(i)
		t := st.Field(i)
		sysPropName := "streaming." + strings.ToLower(streampanel.AppName) + ".flags." + t.Name
		value, err := getSystemProperty(sysPropName)
		if err != nil {
			logger.Errorf(ctx, "unable to get sysprop '%s': %v", sysPropName, err)
			return
		}
		logger.Debugf(ctx, "sysprop '%s' value is '%s'", sysPropName, value)
		if value == "" {
			continue
		}
		switch t.Type.Name() {
		case "logger.Level":
			var l logger.Level
			l.Set(value)
			if l == logger.LevelUndefined {
				logger.Errorf(ctx, "cannot parse the logging level from '%s': %v", value, err)
				continue
			}
			f.Set(reflect.ValueOf(l))
		case "string":
			f.SetString(value)
		case "bool":
			b, err := strconv.ParseBool(value)
			if err != nil {
				logger.Errorf(ctx, "cannot parse the boolean value from '%s': %v", value, err)
				continue
			}
			f.SetBool(b)
		default:
			logger.Errorf(ctx, "not supported type of flag field '%s': %s", t.Name, t.Type.Name())
		}
	}
}

var androidFS = os.DirFS("/")

func getFlagsAndroidFromFiles(flags *Flags) {
	filePathUnexpanded := "~/flags.yaml"
	ctx := context.TODO()
	flagsFilePath, err := xpath.Expand(filePathUnexpanded)
	if err != nil {
		logger.Errorf(ctx, "unable to expand '%s': %v", filePathUnexpanded, err)
		return
	}
	flagsFilePath = strings.Trim(flagsFilePath, "/")

	f, err := androidFS.Open(flagsFilePath)
	if os.IsNotExist(err) {
		logger.Debugf(ctx, "file '%s' does not exist", flagsFilePath)
		return
	}
	defer f.Close()

	flagsSerialized, err := io.ReadAll(f)
	if err != nil {
		logger.Errorf(ctx, "unable to read the file '%s': %v", flagsFilePath, err)
		return
	}

	err = yaml.Unmarshal(flagsSerialized, flags)
	if err != nil {
		logger.Errorf(ctx, "unable to unserialize '%s': %v", flagsSerialized, err)
	}

	logger.Debugf(
		ctx,
		"successfully parsed file '%s' with content '%s'; now the flags == %#+v",
		flagsFilePath,
		flagsSerialized,
		*flags,
	)
}
