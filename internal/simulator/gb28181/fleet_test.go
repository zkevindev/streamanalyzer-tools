package gb28181

import (
	"strings"
	"testing"
)

func TestGB28181BatchIdentifiers(t *testing.T) {
	base := Config{
		PlatformHost: "127.0.0.1", PlatformPort: 5060, PlatformID: "34020000002000000001",
		DeviceID: "34020000001320000001", ChannelID: "34020000001320000002",
		DeviceCount: 2, ChannelsPerDevice: 2, LocalPort: 5061,
	}
	specs, err := deviceSpecs(base)
	if err != nil {
		t.Fatal(err)
	}
	second := specs[1]
	if second.config.DeviceID != "34020000001320000004" || second.config.Username != second.config.DeviceID ||
		second.config.LocalPort != 5062 || len(second.channels) != 2 || second.channels[1] != "34020000001320000006" {
		t.Fatalf("invalid second device: %+v", second)
	}
	shared := base
	shared.Username = base.DeviceID
	specs, err = deviceSpecs(shared)
	if err != nil || specs[1].config.Username != shared.Username {
		t.Fatalf("explicit Digest username must be shared by the batch, got %+v (%v)", specs, err)
	}

	for _, tc := range []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"overlapping identifiers", func(c *Config) { c.ChannelID = "34020000001320000003" }, "编号重复"},
		{"platform identifier collision", func(c *Config) { c.PlatformID = second.channels[1] }, "编号重复"},
		{"serial overflow", func(c *Config) { c.DeviceID = "34020000001329999998" }, "七位序号"},
		{"port overflow", func(c *Config) { c.LocalPort = 65535 }, "端口"},
		{"too many devices", func(c *Config) { c.DeviceCount = maxDevices + 1 }, "设备数"},
		{"invalid start interval", func(c *Config) { c.RegisterIntervalMS = -1 }, "启动间隔"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.change(&cfg)
			if _, err := deviceSpecs(cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error containing %q, got %v", tc.want, err)
			}
		})
	}
}
