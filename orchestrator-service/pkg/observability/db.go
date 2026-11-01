package observability

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

type gormPlugin struct{}

func (gormPlugin) Name() string { return "distri:observability" }

// Initialize hooks every GORM statement: one span and one histogram sample each.
func (gormPlugin) Initialize(db *gorm.DB) error {
	begin := func(op string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			tx.InstanceSet("distri:start", time.Now())
			// Background polling (outbox claim, sweepers) has no parent span; a root
			// span per poll would flood the trace store, so only trace under a request.
			if !trace.SpanContextFromContext(tx.Statement.Context).IsValid() {
				return
			}
			ctx, span := StartSpan(tx.Statement.Context, "db."+op, trace.SpanKindClient, attribute.String("db.operation", op))
			tx.Statement.Context = ctx
			tx.InstanceSet("distri:span", span)
		}
	}
	finish := func(op string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			if v, ok := tx.InstanceGet("distri:start"); ok {
				DBQueryDuration.WithLabelValues(op).Observe(time.Since(v.(time.Time)).Seconds())
			}
			err := tx.Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = nil
			}
			if err != nil {
				DBErrors.WithLabelValues(op, ClassifyDBError(err)).Inc()
			}
			if v, ok := tx.InstanceGet("distri:span"); ok {
				span := v.(trace.Span)
				if stmt := tx.Statement.SQL.String(); stmt != "" {
					if len(stmt) > 300 {
						stmt = stmt[:300]
					}
					span.SetAttributes(attribute.String("db.statement", stmt))
				}
				span.SetAttributes(attribute.Int64("db.rows_affected", tx.RowsAffected))
				EndSpan(span, err)
			}
		}
	}
	cb := db.Callback()
	type registrar interface {
		Register(name string, fn func(*gorm.DB)) error
	}
	hooks := []struct {
		op     string
		before registrar
		after  registrar
	}{
		{"create", cb.Create().Before("gorm:create"), cb.Create().After("gorm:create")},
		{"query", cb.Query().Before("gorm:query"), cb.Query().After("gorm:query")},
		{"update", cb.Update().Before("gorm:update"), cb.Update().After("gorm:update")},
		{"delete", cb.Delete().Before("gorm:delete"), cb.Delete().After("gorm:delete")},
		{"row", cb.Row().Before("gorm:row"), cb.Row().After("gorm:row")},
		{"raw", cb.Raw().Before("gorm:raw"), cb.Raw().After("gorm:raw")},
	}
	for _, h := range hooks {
		if err := h.before.Register("distri:before_"+h.op, begin(h.op)); err != nil {
			return err
		}
		if err := h.after.Register("distri:after_"+h.op, finish(h.op)); err != nil {
			return err
		}
	}
	return nil
}

// InstrumentGorm adds statement spans and metrics, and the connection-pool collector.
func InstrumentGorm(db *gorm.DB, poolName string) error {
	if err := db.Use(gormPlugin{}); err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return Registry.Register(collectors.NewDBStatsCollector(sqlDB, poolName))
}

// ClassifyDBError names the failure kind for the db_errors_total label.
func ClassifyDBError(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40P01":
			return "deadlock"
		case "40001":
			return "serialization_failure"
		case "23505":
			return "unique_violation"
		case "23514":
			return "check_violation"
		case "23503":
			return "foreign_key_violation"
		case "55P03":
			return "lock_not_available"
		}
		return "pg_" + strings.ToLower(pgErr.Code)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, sql.ErrConnDone):
		return "connection"
	}
	return "other"
}

// Transaction runs fn in a GORM transaction under a named span and counts the
// result in db_transactions_total. Keep fn short: no external calls inside.
func Transaction(ctx context.Context, db *gorm.DB, name string, fn func(tx *gorm.DB) error) error {
	span := trace.SpanFromContext(ctx)
	traced := trace.SpanContextFromContext(ctx).IsValid()
	if traced {
		ctx, span = StartSpan(ctx, "db.tx "+name, trace.SpanKindInternal, attribute.String("db.transaction", name))
	}
	err := db.WithContext(ctx).Transaction(fn)
	result := "committed"
	if err != nil {
		result = "failed"
		DBErrors.WithLabelValues("transaction", ClassifyDBError(err)).Inc()
	}
	DBTransactions.WithLabelValues(name, result).Inc()
	if traced {
		EndSpan(span, err)
	}
	return err
}

var _ prometheus.Collector = (*GaugeCollector)(nil)
