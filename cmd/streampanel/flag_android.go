//go:build android
// +build android

package main

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-yaml/yaml"
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
		f := sv.Index(i)
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

func getFlagsAndroidFromFiles(flags *Flags) {
	filePathUnexpanded := "~/flags.json"
	ctx := context.TODO()
	flagsFilePath, err := xpath.Expand(filePathUnexpanded)
	if err != nil {
		logger.Errorf(ctx, "unable to expand '%s': %v", filePathUnexpanded, err)
		return
	}

	_, statErr := os.Stat(flagsFilePath)
	if os.IsNotExist(statErr) {
		logger.Debugf(ctx, "file '%s' does not exist", flagsFilePath)
		return
	}

	flagsSerialized, err := os.ReadFile(flagsFilePath)
	if err != nil {
		logger.Errorf(ctx, "unable to read the file '%s': %v", flagsFilePath, err)
		return
	}
	err = yaml.Unmarshal(flagsSerialized, flags)
	if err != nil {
		logger.Errorf(ctx, "unable to unserialize '%s': %v", flagsSerialized, err)
	}
}
