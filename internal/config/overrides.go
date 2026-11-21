package config

import (
	"errors"
	"fmt"
)

// Overrides carries command-line values.
//
// Pointer fields mean "not specified", so passing -pprof=false does not
// silently undo an environment variable that said true. Every value goes
// through the same parser the environment path uses, so a flag and a variable
// cannot disagree about what is valid.
type Overrides struct {
	HTTPAddr    string
	LogFormat   string
	LogLevel    string
	LogFile     string
	EnablePprof *bool
	DBPath      string
	SeedDemo    *bool
}

// Apply merges command-line overrides into the configuration. Flags win over
// the environment, which is the precedence operators expect: an explicit
// argument is a more recent statement of intent than a variable set at deploy
// time.
func (c *Config) Apply(o Overrides) error {
	var errs []error

	if o.HTTPAddr != "" {
		v, err := parseNonEmptyString(o.HTTPAddr)
		if err != nil {
			errs = append(errs, fmt.Errorf("-http-addr: %w", err))
		} else {
			c.HTTPAddr = v
		}
	}
	if o.LogFormat != "" {
		v, err := parseLogFormat(o.LogFormat)
		if err != nil {
			errs = append(errs, fmt.Errorf("-log-format: %w", err))
		} else {
			c.LogFormat = v
		}
	}
	if o.LogLevel != "" {
		v, err := parseLevel(o.LogLevel)
		if err != nil {
			errs = append(errs, fmt.Errorf("-log-level: %w", err))
		} else {
			c.LogLevel = v
		}
	}
	if o.LogFile != "" {
		c.LogFile = o.LogFile
	}
	if o.EnablePprof != nil {
		c.EnablePprof = *o.EnablePprof
	}
	if o.DBPath != "" {
		c.DBPath = o.DBPath
	}
	if o.SeedDemo != nil {
		c.SeedDemo = *o.SeedDemo
	}

	return errors.Join(errs...)
}
