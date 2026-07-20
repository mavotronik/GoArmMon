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

	bot, err := telegram.New(cfg.Telegram, e.cache)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	e.bot = bot

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
	e.bot.NotifyStartup(e.cfgPath)
	updates := e.bot.UpdatesChannel()
	e.bot.Run(ctx, updates, e.alerts.Events())

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
