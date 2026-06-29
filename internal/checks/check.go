package checks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"goarmmon/internal/config"
	"goarmmon/internal/state"

	"gopkg.in/yaml.v3"
)

type Runner interface {
	ID() string
	Type() string
	HostName() string
	Interval() time.Duration
	Run(ctx context.Context) state.CheckSnapshot
}

type Factory func(host config.HostConfig, raw yaml.Node) (Runner, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

func Register(typ string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = factory
}

func New(typ string, host config.HostConfig, raw yaml.Node) (Runner, error) {
	mu.RLock()
	factory, ok := factories[typ]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown check type %q", typ)
	}
	return factory(host, raw)
}

func BuildAll(host config.HostConfig) ([]Runner, error) {
	defs := host.CheckDefinitions()
	runners := make([]Runner, 0, len(defs))
	for _, def := range defs {
		r, err := New(def.Type, host, def.Raw)
		if err != nil {
			return nil, fmt.Errorf("host %q: %w", host.Name, err)
		}
		runners = append(runners, r)
	}
	return runners, nil
}

func CheckID(host, typ string) string {
	return host + ":" + typ
}
