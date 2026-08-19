package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"goarmmon/internal/alerts"
	"goarmmon/internal/config"
	"goarmmon/internal/logger"
	"goarmmon/internal/scheduler"
	"goarmmon/internal/state"
	"goarmmon/internal/telegram"
)

type Engine struct {
	cfgPath string
	log     *logger.Logger
	cache   *state.Cache
	alerts  *alerts.Manager
	sched   *scheduler.Scheduler
	bot     *telegram.Bot
	mu      sync.Mutex
	cfg     *config.Config
	runCtx  context.Context
}

func NewEngine(cfgPath string) *Engine {
	return &Engine{
		cfgPath: cfgPath,
		cache:   state.NewCache(),
	}
}

func (e *Engine) Run(ctx context.Context) error {
	cfg, err := config.Load(e.cfgPath)
	if err != nil {
		return err
	}

	log, err := logger.New(cfg.Logging.Level, cfg.Logging.File)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	e.log = log
	e.cfg = cfg

	e.alerts = alerts.NewManager(cfg.Hosts, 256)
	e.sched = scheduler.New(e.cache, e.alerts)

	bot := telegram.New(cfg.Telegram, e.cache)
	e.bot = bot
	bot.SetStartupConfigPath(e.cfgPath)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.runCtx = ctx
	bot.SetConfigStore(e, ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			slog.Info("shutdown signal received")
			cancel()
		case <-ctx.Done():
		}
	}()

	e.sched.ApplyConfig(ctx, cfg)

	if err := config.Watch(ctx, e.cfgPath, func(newCfg *config.Config) {
		e.onConfigReload(ctx, newCfg)
	}); err != nil {
		return fmt.Errorf("config watch: %w", err)
	}

	slog.Info("monitor started", "config", e.cfgPath)
	e.bot.Run(ctx, e.alerts.Events())

	e.sched.Stop()
	return nil
}

func (e *Engine) onConfigReload(ctx context.Context, cfg *config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.log.Configure(cfg.Logging.Level, cfg.Logging.File); err != nil {
		slog.Warn("logger reconfigure failed", "error", err)
	}

	e.cfg = cfg
	e.bot.UpdateConfig(cfg.Telegram)
	e.sched.ApplyConfig(ctx, cfg)
}

func (e *Engine) HostConfigs() []config.HostConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg == nil {
		return nil
	}
	out := make([]config.HostConfig, len(e.cfg.Hosts))
	copy(out, e.cfg.Hosts)
	return out
}

func (e *Engine) HostConfig(name string) (config.HostConfig, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg == nil {
		return config.HostConfig{}, false
	}
	for _, h := range e.cfg.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return config.HostConfig{}, false
}

func (e *Engine) MutateConfig(fn func(*config.Config) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	clone, err := config.Clone(e.cfg)
	if err != nil {
		return fmt.Errorf("clone config: %w", err)
	}
	if err := fn(clone); err != nil {
		return err
	}
	if err := config.Save(e.cfgPath, clone); err != nil {
		return err
	}

	ctx := e.runCtx
	if ctx == nil {
		ctx = context.Background()
	}

	if err := e.log.Configure(clone.Logging.Level, clone.Logging.File); err != nil {
		slog.Warn("logger reconfigure failed", "error", err)
	}
	e.cfg = clone
	e.bot.UpdateConfig(clone.Telegram)
	e.sched.ApplyConfig(ctx, clone)
	return nil
}
