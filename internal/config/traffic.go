package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/platform/traffic"
)

func loadTraffic(getenv func(string) string, c *Config) error {
	l := traffic.DefaultLimits()
	parse := func(key string, fallback int) (int, error) {
		raw := getenv(key)
		if raw == "" {
			return fallback, nil
		}
		if len(raw) > 5 || strings.Trim(raw, "0123456789") != "" {
			return 0, fmt.Errorf("%s must be a bounded positive integer", key)
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("%s must be a bounded positive integer", key)
		}
		return n, nil
	}
	var err error
	if l.PeerRate, err = parse("API_REQUESTS_PER_SECOND", l.PeerRate); err != nil {
		return err
	}
	if l.PeerBurst, err = parse("API_REQUEST_BURST", 2*l.PeerRate); err != nil {
		return err
	}
	if l.GlobalRate, err = parse("API_GLOBAL_REQUESTS_PER_SECOND", l.GlobalRate); err != nil {
		return err
	}
	if l.GlobalBurst, err = parse("API_GLOBAL_REQUEST_BURST", 2*l.GlobalRate); err != nil {
		return err
	}
	if l.Validate() != nil {
		return fmt.Errorf("API request rates must be 1..10000, bursts 1..2*rate and peer bounds no larger than global")
	}
	c.APIRequestLimits = l
	return nil
}
