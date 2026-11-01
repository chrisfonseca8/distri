package observability

import (
	"context"
	"os"
	"time"

	"gorm.io/gorm"
)

// Options configures Setup.
type Options struct {
	Service string
	// MetricsAddr is the default /metrics listen address (METRICS_ADDR overrides;
	// "off" disables it).
	MetricsAddr string
	DB          *gorm.DB
	PoolName    string
}

// Setup is called once from main after the database is open. It installs the
// logger, common metrics, GORM instrumentation, readiness, tracing and the
// metrics listener. The returned function flushes traces and stops the listener.
func Setup(opts Options) (func(), error) {
	SetProcess(Config{Service: opts.Service})
	InitMetrics()
	if opts.DB != nil {
		if err := InstrumentGorm(opts.DB, opts.PoolName); err != nil {
			return nil, err
		}
		if sqlDB, err := opts.DB.DB(); err == nil {
			SetReadinessCheck(sqlDB.PingContext)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	shutdownTracing, err := InitTracing(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	addr := opts.MetricsAddr
	switch v := os.Getenv("METRICS_ADDR"); v {
	case "off":
		addr = ""
	case "":
	default:
		addr = v
	}
	ServeMetrics(ctx, addr)
	return func() {
		cancel()
		flush, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = shutdownTracing(flush)
	}, nil
}
