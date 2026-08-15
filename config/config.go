// Package config loads sources and rules from YAML and validates that they
// refer to each other consistently.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zxc0zxc0zxc/reconciliation/recon"
)

// Config is a whole setup: where the data comes from and how it is compared.
type Config struct {
	Sources map[string]Source `yaml:"sources"`
	Rules   []Rule            `yaml:"rules"`
}

// Source is one gRPC endpoint implementing the RecordSource service.
type Source struct {
	Address string `yaml:"address"`
	// Timeout bounds a single collection, as a Go duration. Empty means DefaultTimeout.
	Timeout string `yaml:"timeout"`
	// PageSize is the number of records requested per page. Zero means DefaultPageSize.
	PageSize int32 `yaml:"page_size"`
	// TLS dials the endpoint with system root certificates instead of plaintext.
	TLS bool `yaml:"tls"`
}

// Rule is the YAML shape of recon.Rule.
type Rule struct {
	Name               string     `yaml:"name"`
	Sides              []string   `yaml:"sides"`
	Mode               string     `yaml:"mode"`
	AmountTolerance    int64      `yaml:"amount_tolerance"`
	InFlightWindow     string     `yaml:"in_flight_window"`
	RequireStatusMatch bool       `yaml:"require_status_match"`
	Thresholds         Thresholds `yaml:"thresholds"`
}

// Thresholds maps an amount, in minor units, onto a severity.
type Thresholds struct {
	Medium   int64 `yaml:"medium"`
	High     int64 `yaml:"high"`
	Critical int64 `yaml:"critical"`
}

// Endpoint is a source with defaults applied and durations resolved.
type Endpoint struct {
	Name     string
	Address  string
	Timeout  time.Duration
	PageSize int32
	TLS      bool
}

// Values applied to a source that leaves them unset.
const (
	DefaultTimeout  = 30 * time.Second
	DefaultPageSize = 500
)

// ErrNotFound reports a reference to a rule or source the config does not define.
var ErrNotFound = errors.New("not found in config")

// Load reads and validates a configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Sources) == 0 {
		return errors.New("no sources defined")
	}
	for name, src := range c.Sources {
		if _, err := src.endpoint(name); err != nil {
			return err
		}
	}
	if len(c.Rules) == 0 {
		return errors.New("no rules defined")
	}

	seen := make(map[string]struct{}, len(c.Rules))
	for _, raw := range c.Rules {
		if _, dup := seen[raw.Name]; dup {
			return fmt.Errorf("rule %q: defined twice", raw.Name)
		}
		seen[raw.Name] = struct{}{}

		rule, err := raw.Rule()
		if err != nil {
			return err
		}
		if err := rule.Validate(); err != nil {
			return err
		}
		for _, side := range rule.Sides {
			if _, ok := c.Sources[side]; !ok {
				return fmt.Errorf("rule %q: source %q %w", rule.Name, side, ErrNotFound)
			}
		}
	}
	return nil
}

// Rule converts the YAML shape into the engine rule.
func (r Rule) Rule() (recon.Rule, error) {
	if len(r.Sides) != 2 {
		return recon.Rule{}, fmt.Errorf("rule %q: sides must list exactly two sources", r.Name)
	}
	window, err := parseDuration(r.InFlightWindow)
	if err != nil {
		return recon.Rule{}, fmt.Errorf("rule %q: in_flight_window: %w", r.Name, err)
	}
	return recon.Rule{
		Name:               r.Name,
		Sides:              [2]string{r.Sides[0], r.Sides[1]},
		Mode:               recon.Mode(r.Mode),
		AmountTolerance:    r.AmountTolerance,
		InFlightWindow:     window,
		RequireStatusMatch: r.RequireStatusMatch,
		Thresholds: recon.SeverityThresholds{
			Medium:   r.Thresholds.Medium,
			High:     r.Thresholds.High,
			Critical: r.Thresholds.Critical,
		},
	}, nil
}

// Lookup returns the rule by name together with the endpoints of its two sides.
func (c *Config) Lookup(name string) (recon.Rule, [2]Endpoint, error) {
	var endpoints [2]Endpoint
	for _, raw := range c.Rules {
		if raw.Name != name {
			continue
		}
		rule, err := raw.Rule()
		if err != nil {
			return recon.Rule{}, endpoints, err
		}
		for i, side := range rule.Sides {
			endpoint, err := c.Sources[side].endpoint(side)
			if err != nil {
				return recon.Rule{}, endpoints, err
			}
			endpoints[i] = endpoint
		}
		return rule, endpoints, nil
	}
	return recon.Rule{}, endpoints, fmt.Errorf("rule %q %w", name, ErrNotFound)
}

// RuleNames lists the configured rules in file order.
func (c *Config) RuleNames() []string {
	names := make([]string, 0, len(c.Rules))
	for _, r := range c.Rules {
		names = append(names, r.Name)
	}
	return names
}

func (s Source) endpoint(name string) (Endpoint, error) {
	if s.Address == "" {
		return Endpoint{}, fmt.Errorf("source %q: address is empty", name)
	}
	timeout, err := parseDuration(s.Timeout)
	if err != nil {
		return Endpoint{}, fmt.Errorf("source %q: timeout: %w", name, err)
	}
	if timeout < 0 {
		return Endpoint{}, fmt.Errorf("source %q: timeout must not be negative", name)
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if s.PageSize < 0 {
		return Endpoint{}, fmt.Errorf("source %q: page_size must not be negative", name)
	}
	pageSize := s.PageSize
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	return Endpoint{Name: name, Address: s.Address, Timeout: timeout, PageSize: pageSize, TLS: s.TLS}, nil
}

func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}
