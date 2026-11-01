package reply

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/businessmetrics"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/observability"
)

// replyObservation is what one committed reply changed. It is filled inside the
// transaction and recorded only after the commit, so rolled-back work is never counted.
type replyObservation struct {
	changed       bool
	sagaCreatedAt time.Time
	step          string
	outcome       string
	stepStartedAt *time.Time
	nextStep      string
	statusBefore  string
	statusAfter   string
	resend        bool
}

func (o replyObservation) record(ctx context.Context) {
	if !o.changed || o.step == "" {
		return
	}
	businessmetrics.StepCompletions.WithLabelValues(o.step, o.outcome).Inc()
	var took float64
	if o.stepStartedAt != nil {
		took = time.Since(*o.stepStartedAt).Seconds()
		businessmetrics.StepDuration.WithLabelValues(o.step, o.outcome).Observe(took)
	}
	if businessmetrics.CompensationSteps[o.step] {
		businessmetrics.Compensations.WithLabelValues(o.step, o.outcome).Inc()
		if o.stepStartedAt != nil {
			businessmetrics.CompensationDuration.WithLabelValues(o.step).Observe(took)
		}
	}
	observability.Event(ctx, slog.LevelInfo, observability.EventSagaStepCompleted, "saga step completed",
		"step", o.step, "outcome", o.outcome, "duration_seconds", took)
	if o.resend {
		businessmetrics.StepRetries.WithLabelValues(o.step).Inc()
	}
	if o.nextStep != "" && !o.resend {
		observability.Event(ctx, slog.LevelInfo, observability.EventSagaStepStarted, "saga step started", "step", o.nextStep)
	}
	if o.statusAfter == "COMPENSATING" && o.statusBefore != "COMPENSATING" {
		businessmetrics.CompensationsStarted.Inc()
	}
	if o.statusAfter != o.statusBefore && (o.statusAfter == "COMPLETED" || o.statusAfter == "COMPENSATED") {
		businessmetrics.SagaCompletions.WithLabelValues(o.statusAfter).Inc()
		businessmetrics.SagaDuration.WithLabelValues(o.statusAfter).Observe(time.Since(o.sagaCreatedAt).Seconds())
	}
	if o.statusAfter == "STUCK" && o.statusBefore != "STUCK" {
		businessmetrics.StuckTransitions.WithLabelValues("reply").Inc()
	}
}
