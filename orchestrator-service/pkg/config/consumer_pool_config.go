package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultConsumerScaleInterval  = 5 * time.Second
	defaultConsumerDepthPerWorker = 20
	defaultConsumerScaleDownAfter = 30 * time.Second
)

// ConsumerPoolConfig lets a queue's consumer count follow its backlog. With
// MaxConcurrency equal to the minimum (the default) the pool never scales.
type ConsumerPoolConfig struct {
	MaxConcurrency int
	ScaleInterval  time.Duration
	DepthPerWorker int
	ScaleDownAfter time.Duration
}

// loadConsumerPoolConfig reads a consumer pool's maximum concurrency and scaling interval using the given env prefix.
func loadConsumerPoolConfig(prefix string, minimum int) (ConsumerPoolConfig, error) {
	maximum, err := envInt(prefix+"_CONSUMER_MAX_CONCURRENCY", minimum)
	if err != nil {
		return ConsumerPoolConfig{}, err
	}
	interval, err := envDuration(prefix+"_CONSUMER_SCALE_INTERVAL", defaultConsumerScaleInterval)
	if err != nil {
		return ConsumerPoolConfig{}, err
	}
	depthPerWorker, err := envInt(prefix+"_CONSUMER_SCALE_DEPTH_PER_WORKER", defaultConsumerDepthPerWorker)
	if err != nil {
		return ConsumerPoolConfig{}, err
	}
	scaleDownAfter, err := envDuration(prefix+"_CONSUMER_SCALE_DOWN_AFTER", defaultConsumerScaleDownAfter)
	if err != nil {
		return ConsumerPoolConfig{}, err
	}
	if maximum < minimum {
		return ConsumerPoolConfig{}, fmt.Errorf(
			"%s_CONSUMER_MAX_CONCURRENCY must not be below %s_CONSUMER_CONCURRENCY",
			prefix, prefix,
		)
	}
	if interval <= 0 {
		return ConsumerPoolConfig{}, errors.New(prefix + "_CONSUMER_SCALE_INTERVAL must be greater than zero")
	}
	if depthPerWorker <= 0 {
		return ConsumerPoolConfig{}, errors.New(prefix + "_CONSUMER_SCALE_DEPTH_PER_WORKER must be greater than zero")
	}
	if scaleDownAfter < 0 {
		return ConsumerPoolConfig{}, errors.New(prefix + "_CONSUMER_SCALE_DOWN_AFTER must not be negative")
	}
	return ConsumerPoolConfig{
		MaxConcurrency: maximum,
		ScaleInterval:  interval,
		DepthPerWorker: depthPerWorker,
		ScaleDownAfter: scaleDownAfter,
	}, nil
}
