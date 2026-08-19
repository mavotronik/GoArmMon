package telegram

import "goarmmon/internal/config"

// ConfigStore provides read/write access to the monitor configuration.
type ConfigStore interface {
	HostConfigs() []config.HostConfig
	HostConfig(name string) (config.HostConfig, bool)
	MutateConfig(fn func(*config.Config) error) error
}
