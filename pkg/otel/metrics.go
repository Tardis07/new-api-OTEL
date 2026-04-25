package otel

import (
	"github.com/QuantumNous/new-api/common"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// ── Metric Instruments ──
var (
	// RequestCounter counts relay requests by method, status, model, provider, and stream mode.
	RequestCounter metric.Int64Counter

	// RequestDuration records relay request duration in milliseconds.
	RequestDuration metric.Float64Histogram

	// InputTokens records prompt/input token usage.
	InputTokens metric.Int64Histogram

	// OutputTokens records completion/output token usage.
	OutputTokens metric.Int64Histogram

	// TotalTokens records total token usage (input + output).
	TotalTokens metric.Int64Histogram

	// TimeToFirstToken records time-to-first-token for streaming requests in milliseconds.
	TimeToFirstToken metric.Float64Histogram

	// TimePerOutputToken records time-per-output-token for streaming in milliseconds.
	TimePerOutputToken metric.Float64Histogram
)

func initMetrics() {
	meter := otel.Meter("new-api")

	var err error

	RequestCounter, err = meter.Int64Counter(
		"newapi.relay.request.total",
		metric.WithDescription("Total number of relay requests"),
	)
	if err != nil {
		common.SysError("otel: failed to create RequestCounter: " + err.Error())
	}

	RequestDuration, err = meter.Float64Histogram(
		"newapi.relay.request.duration",
		metric.WithDescription("Relay request duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		common.SysError("otel: failed to create RequestDuration: " + err.Error())
	}

	InputTokens, err = meter.Int64Histogram(
		"gen_ai.client.token.usage",
		metric.WithDescription("Number of input (prompt) tokens used"),
		metric.WithUnit("token"),
	)
	if err != nil {
		common.SysError("otel: failed to create InputTokens: " + err.Error())
	}

	OutputTokens, err = meter.Int64Histogram(
		"gen_ai.client.token.usage",
		metric.WithDescription("Number of output (completion) tokens used"),
		metric.WithUnit("token"),
	)
	if err != nil {
		common.SysError("otel: failed to create OutputTokens: " + err.Error())
	}

	TotalTokens, err = meter.Int64Histogram(
		"newapi.relay.token.total",
		metric.WithDescription("Total token usage (input + output)"),
		metric.WithUnit("token"),
	)
	if err != nil {
		common.SysError("otel: failed to create TotalTokens: " + err.Error())
	}

	TimeToFirstToken, err = meter.Float64Histogram(
		"gen_ai.client.response.time_to_first_token",
		metric.WithDescription("Time to first token for streaming requests in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		common.SysError("otel: failed to create TimeToFirstToken: " + err.Error())
	}

	TimePerOutputToken, err = meter.Float64Histogram(
		"gen_ai.client.response.time_per_output_token",
		metric.WithDescription("Average time per output token in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		common.SysError("otel: failed to create TimePerOutputToken: " + err.Error())
	}
}
