package executor

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Validate(params StartParams) error {
	if params.Command == "" {
		return errors.New("command is required")
	}
	if strings.IndexByte(params.Command, 0) >= 0 {
		return errors.New("command contains NUL byte")
	}
	if params.Cwd != "" {
		info, err := os.Stat(params.Cwd)
		if err != nil {
			return errors.New("cwd does not exist")
		}
		if !info.IsDir() {
			return errors.New("cwd is not a directory")
		}
	}
	if params.Shell != "" && strings.IndexByte(params.Shell, 0) >= 0 {
		return errors.New("shell contains NUL byte")
	}
	for name, value := range params.Env {
		if !envNamePattern.MatchString(name) {
			return errors.New("invalid environment variable name: " + name)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return errors.New("environment variable contains NUL byte: " + name)
		}
	}
	if params.TimeoutMS < 0 {
		return errors.New("timeout_ms cannot be negative")
	}
	if params.TimeoutMS > (1<<63-1)/int64(time.Millisecond) {
		return errors.New("timeout_ms is too large")
	}
	if params.OutputLimitBytes < 0 {
		return errors.New("output_limit_bytes cannot be negative")
	}
	return nil
}
