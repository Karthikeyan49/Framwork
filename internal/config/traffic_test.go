package config

import (
	"github.com/Karthikeyan49/Framwork/internal/platform/traffic"
	"testing"
)

func TestAPIRequestAdmissionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		values map[string]string
		valid  bool
	}{{nil, true}, {map[string]string{"API_REQUESTS_PER_SECOND": "1"}, true}, {map[string]string{"API_REQUESTS_PER_SECOND": "0"}, false}, {map[string]string{"API_REQUESTS_PER_SECOND": "+1"}, false}, {map[string]string{"API_REQUESTS_PER_SECOND": " 1"}, false}, {map[string]string{"API_REQUEST_BURST": "10000"}, false}, {map[string]string{"API_GLOBAL_REQUESTS_PER_SECOND": "1"}, false}, {map[string]string{"API_REQUESTS_PER_SECOND": "10001"}, false}, {map[string]string{"API_REQUEST_BURST": "private"}, false}} {
		var c Config
		err := loadTraffic(func(k string) string { return tc.values[k] }, &c)
		if (err == nil) != tc.valid {
			t.Fatal("configuration admission mismatch")
		}
		if tc.values == nil && c.APIRequestLimits != traffic.DefaultLimits() {
			t.Fatal("default limits drift")
		}
	}
}
