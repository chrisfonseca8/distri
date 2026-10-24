package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// PoolConfig sizes a consumer pool between Min and Max workers. Each worker is
// one goroutine with its own connection. Scaling follows the number of READY
// messages (unacked ones are already being worked on), so it only reacts to
// backlog, not to in-flight work.
type PoolConfig struct {
	URL            string
	Queue          string
	Min            int
	Max            int
	Interval       time.Duration
	DepthPerWorker int
	ScaleDownAfter time.Duration
}

// validate checks that the pool settings are complete and consistent.
func (c PoolConfig) validate() error {
	switch {
	case c.URL == "":
		return errors.New("consumer pool URL is required")
	case c.Queue == "":
		return errors.New("consumer pool queue is required")
	case c.Min <= 0:
		return errors.New("consumer pool minimum must be greater than zero")
	case c.Max < c.Min:
		return errors.New("consumer pool maximum must not be below the minimum")
	case c.Interval <= 0:
		return errors.New("consumer pool scale interval must be greater than zero")
	case c.DepthPerWorker <= 0:
		return errors.New("consumer pool depth per worker must be greater than zero")
	case c.ScaleDownAfter < 0:
		return errors.New("consumer pool scale-down delay must not be negative")
	}
	return nil
}

// Pool runs between Min and Max consumers on one queue. newConsumer builds a
// fresh consumer for every worker; the initial Min are built eagerly so a bad
// configuration fails at startup.
type Pool struct {
	config      PoolConfig
	newConsumer func() (*Consumer, error)
	initial     []*Consumer
}

// NewPool creates a consumer pool that builds consumers with newConsumer.
func NewPool(config PoolConfig, newConsumer func() (*Consumer, error)) (*Pool, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if newConsumer == nil {
		return nil, errors.New("consumer pool factory is required")
	}
	pool := &Pool{config: config, newConsumer: newConsumer}
	for range config.Min {
		consumer, err := newConsumer()
		if err != nil {
			return nil, err
		}
		pool.initial = append(pool.initial, consumer)
	}
	return pool, nil
}

// desiredWorkers is Min plus one worker per DepthPerWorker ready messages,
// capped at Max.
func desiredWorkers(depth int, config PoolConfig) int {
	target := config.Min + (depth+config.DepthPerWorker-1)/config.DepthPerWorker
	if target > config.Max {
		return config.Max
	}
	return target
}

// Run starts the minimum consumers and adds or removes some as the queue depth changes, until ctx is cancelled.
func (p *Pool) Run(ctx context.Context) {
	slog.Info(
		"RabbitMQ consumer pool started",
		"queue", p.config.Queue,
		"min", p.config.Min,
		"max", p.config.Max,
	)
	defer slog.Info("RabbitMQ consumer pool stopped", "queue", p.config.Queue)

	var workers sync.WaitGroup
	var stops []chan struct{}
	start := func(consumer *Consumer) {
		stop := make(chan struct{})
		stops = append(stops, stop)
		workers.Add(1)
		go func() {
			defer workers.Done()
			consumer.RunUntil(ctx, stop)
		}()
	}
	defer workers.Wait()

	for _, consumer := range p.initial {
		start(consumer)
	}
	if p.config.Max == p.config.Min {
		<-ctx.Done()
		return
	}

	sampler := &depthSampler{url: p.config.URL, queue: p.config.Queue}
	defer sampler.close()
	lastChange := time.Now()
	ticker := time.NewTicker(p.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		depth, err := sampler.depth()
		if err != nil {
			slog.Warn("sample RabbitMQ queue depth", "queue", p.config.Queue, "error", err)
			continue
		}
		current := len(stops)
		target := desiredWorkers(depth, p.config)
		switch {
		case target > current:
			for len(stops) < target {
				consumer, err := p.newConsumer()
				if err != nil {
					slog.Error("create scaled consumer", "queue", p.config.Queue, "error", err)
					break
				}
				start(consumer)
			}
			lastChange = time.Now()
			slog.Info(
				"scaled consumers up",
				"queue", p.config.Queue,
				"ready", depth,
				"workers", len(stops),
			)
		case target < current && time.Since(lastChange) >= p.config.ScaleDownAfter:
			// One at a time: a drained queue should not collapse the pool
			// right before the next burst.
			close(stops[len(stops)-1])
			stops = stops[:len(stops)-1]
			lastChange = time.Now()
			slog.Info(
				"scaled consumers down",
				"queue", p.config.Queue,
				"ready", depth,
				"workers", len(stops),
			)
		}
	}
}

// depthSampler reads the READY count with a passive declare, which never
// creates or changes the queue (consumers must not declare queues). A failed
// passive declare closes its channel, so any error drops the connection.
type depthSampler struct {
	url        string
	queue      string
	connection *amqp.Connection
	channel    *amqp.Channel
}

// depth returns the number of messages waiting in the queue, connecting first if needed.
func (s *depthSampler) depth() (int, error) {
	if s.channel == nil {
		connection, err := amqp.Dial(s.url)
		if err != nil {
			return 0, fmt.Errorf("connect queue depth sampler: %w", err)
		}
		channel, err := connection.Channel()
		if err != nil {
			_ = connection.Close()
			return 0, fmt.Errorf("open queue depth channel: %w", err)
		}
		s.connection, s.channel = connection, channel
	}
	queue, err := s.channel.QueueDeclarePassive(s.queue, true, false, false, false, nil)
	if err != nil {
		s.close()
		return 0, fmt.Errorf("inspect queue %q: %w", s.queue, err)
	}
	return queue.Messages, nil
}

// close drops the sampler's broker connection.
func (s *depthSampler) close() {
	if s.connection != nil {
		_ = s.connection.Close()
	}
	s.connection, s.channel = nil, nil
}
