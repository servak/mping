package command

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/spf13/pflag"

	"github.com/servak/mping/internal/config"
)

// parseTimingFlags reads and validates the --interval and --timeout flags
// (both in milliseconds) shared by the mping and batch commands.
func parseTimingFlags(flags *pflag.FlagSet) (interval, timeout time.Duration, err error) {
	intervalMs, err := flags.GetInt("interval")
	if err != nil {
		return 0, 0, err
	}
	timeoutMs, err := flags.GetInt("timeout")
	if err != nil {
		return 0, 0, err
	}
	return validateTiming(intervalMs, timeoutMs)
}

// validateTiming rejects non-positive values and converts milliseconds to durations.
func validateTiming(intervalMs, timeoutMs int) (time.Duration, time.Duration, error) {
	switch {
	case intervalMs <= 0 && timeoutMs <= 0:
		return 0, 0, errors.New("both interval and timeout must be greater than zero")
	case intervalMs <= 0:
		return 0, 0, errors.New("interval must be greater than zero")
	case timeoutMs <= 0:
		return 0, 0, errors.New("timeout must be greater than zero")
	}
	return time.Duration(intervalMs) * time.Millisecond, time.Duration(timeoutMs) * time.Millisecond, nil
}

// loadConfig loads the config file. A missing file falls back to defaults
// only when the path was not given explicitly (the implicit ~/.mping.yml may
// legitimately be absent); an explicit path that cannot be read is an error.
func loadConfig(path string, explicit bool) (*config.Config, error) {
	cfg, err := config.LoadFile(path)
	if cfg == nil {
		return nil, fmt.Errorf("failed to load config %q: %w", path, err)
	}
	if err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return nil, fmt.Errorf("failed to load config %q: %w", path, err)
	}
	return cfg, nil
}
