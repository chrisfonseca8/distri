package observability

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// GaugeSpec is one scrape-time gauge computed by a SQL query. The query returns
// len(Labels) text columns followed by one numeric column. Zero lists label
// combinations that are emitted as 0 when the query returns no row for them, so
// "no STUCK sagas" is a 0, not a missing series.
type GaugeSpec struct {
	Name   string
	Help   string
	Labels []string
	Zero   [][]string
	SQL    string
	// After, when set, sees the rows each time the query runs (for logging events).
	After func(ctx context.Context, rows []GaugeRow)
}

// GaugeRow is one result row of a GaugeSpec.
type GaugeRow struct {
	Labels []string
	Value  float64
}

// GaugeCollector runs its queries on scrape (cached for ttl) so business gauges
// reflect PostgreSQL, the authority, without per-event bookkeeping in the app.
type GaugeCollector struct {
	db    *sql.DB
	specs []GaugeSpec
	descs []*prometheus.Desc
	ttl   time.Duration

	mu      sync.Mutex
	fetched time.Time
	cache   [][]GaugeRow
	failed  []bool
}

var scrapeErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metrics_query_errors_total", Help: "Scrape-time gauge queries that failed."}, []string{"metric"})

func init() { Registry.MustRegister(scrapeErrors) }

// NewGaugeCollector registers a collector for specs. ttl bounds how often the
// database is queried regardless of scrape frequency.
func NewGaugeCollector(db *sql.DB, ttl time.Duration, specs ...GaugeSpec) *GaugeCollector {
	c := &GaugeCollector{db: db, specs: specs, ttl: ttl}
	for _, s := range specs {
		c.descs = append(c.descs, prometheus.NewDesc(s.Name, s.Help, s.Labels, nil))
	}
	Registry.MustRegister(c)
	return c
}

func (c *GaugeCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

func (c *GaugeCollector) refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache != nil && time.Since(c.fetched) < c.ttl {
		return
	}
	cache := make([][]GaugeRow, len(c.specs))
	failed := make([]bool, len(c.specs))
	for i, s := range c.specs {
		rows, err := c.query(s)
		if err != nil {
			failed[i] = true
			scrapeErrors.WithLabelValues(s.Name).Inc()
			slog.Warn("gauge query failed", "metric", s.Name, "error", err)
			continue
		}
		cache[i] = rows
		if s.After != nil {
			s.After(context.Background(), rows)
		}
	}
	c.cache, c.failed, c.fetched = cache, failed, time.Now()
}

func (c *GaugeCollector) query(s GaugeSpec) ([]GaugeRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := c.db.QueryContext(ctx, s.SQL)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []GaugeRow
	for r.Next() {
		labels := make([]sql.NullString, len(s.Labels))
		var value sql.NullFloat64
		dest := make([]any, 0, len(s.Labels)+1)
		for i := range labels {
			dest = append(dest, &labels[i])
		}
		dest = append(dest, &value)
		if err := r.Scan(dest...); err != nil {
			return nil, err
		}
		row := GaugeRow{Value: value.Float64}
		for _, l := range labels {
			row.Labels = append(row.Labels, l.String)
		}
		out = append(out, row)
	}
	return out, r.Err()
}

func (c *GaugeCollector) Collect(ch chan<- prometheus.Metric) {
	c.refresh()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.specs {
		if c.failed[i] {
			continue
		}
		seen := map[string]bool{}
		for _, row := range c.cache[i] {
			ch <- prometheus.MustNewConstMetric(c.descs[i], prometheus.GaugeValue, row.Value, row.Labels...)
			seen[key(row.Labels)] = true
		}
		for _, z := range s.Zero {
			if !seen[key(z)] {
				ch <- prometheus.MustNewConstMetric(c.descs[i], prometheus.GaugeValue, 0, z...)
			}
		}
	}
}

func key(labels []string) string {
	k := ""
	for _, l := range labels {
		k += l + "\x00"
	}
	return k
}

// OutboxSpecs returns the standard outbox gauges for table (e.g. "orders.outbox").
// Dead rows are those the relay gave up on; pending excludes them.
func OutboxSpecs(table string) []GaugeSpec {
	return []GaugeSpec{
		{Name: "outbox_pending_rows", Help: "Unpublished, non-dead outbox rows.",
			SQL: `SELECT count(*)::float8 FROM ` + table + ` WHERE published_at IS NULL AND dead_at IS NULL`},
		{Name: "outbox_dead_rows", Help: "Outbox rows the relay marked dead (need an operator).",
			SQL: `SELECT count(*)::float8 FROM ` + table + ` WHERE published_at IS NULL AND dead_at IS NOT NULL`},
		{Name: "outbox_oldest_pending_age_seconds", Help: "Age of the oldest unpublished, non-dead outbox row; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 FROM ` + table + ` WHERE published_at IS NULL AND dead_at IS NULL`},
	}
}
