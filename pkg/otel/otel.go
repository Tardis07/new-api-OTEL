package otel

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
	enabled        bool
)

// Init initializes the OpenTelemetry SDK.
// It reads configuration from environment variables:
//
//	OTEL_ENABLED                  - enable/disable OTEL (default: false)
//	OTEL_EXPORTER_OTLP_ENDPOINT   - collector endpoint (default: http://localhost:4318)
//	OTEL_SERVICE_NAME             - service name (default: new-api)
//	OTEL_ENVIRONMENT              - deployment environment (default: production)
func Init(ctx context.Context) error {
	enabled = common.GetEnvOrDefaultBool("OTEL_ENABLED", false)
	if !enabled {
		common.SysLog("OpenTelemetry disabled")
		return nil
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4318"
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "new-api"
	}

	hostname, _ := os.Hostname()

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(common.Version),
			semconv.DeploymentEnvironment(getEnvOrDefault("OTEL_ENVIRONMENT", "production")),
			semconv.HostName(hostname),
		),
	)
	if err != nil {
		return err
	}

	host, port := parseEndpoint(endpoint)
	ep := host + ":" + port

	traceExporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(ep),
	}
	metricExporterOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(ep),
	}
	if !strings.HasPrefix(endpoint, "https") {
		traceExporterOpts = append(traceExporterOpts, otlptracehttp.WithInsecure())
		metricExporterOpts = append(metricExporterOpts, otlpmetrichttp.WithInsecure())
	}

	traceExporter, err := otlptracehttp.New(ctx, traceExporterOpts...)
	if err != nil {
		return err
	}

	TracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	otel.SetTracerProvider(TracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	metricExporter, err := otlpmetrichttp.New(ctx, metricExporterOpts...)
	if err != nil {
		return err
	}

	MeterProvider = metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(15*time.Second),
		)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(MeterProvider)

	initMetrics()

	common.SysLog("OpenTelemetry initialized → " + endpoint)
	return nil
}

// Shutdown flushes and shuts down the OTEL providers.
func Shutdown(ctx context.Context) {
	if !enabled {
		return
	}
	if TracerProvider != nil {
		_ = TracerProvider.Shutdown(ctx)
	}
	if MeterProvider != nil {
		_ = MeterProvider.Shutdown(ctx)
	}
}

// IsEnabled returns whether OTEL is enabled.
func IsEnabled() bool {
	return enabled
}

// parseEndpoint extracts host and port from an OTLP HTTP endpoint URL.
// Handles: "http://host:port", "https://host:port", "host:port", "http://host"
func parseEndpoint(endpoint string) (host, port string) {
	e := endpoint
	if strings.HasPrefix(e, "http://") {
		e = e[7:]
	} else if strings.HasPrefix(e, "https://") {
		e = e[8:]
	}

	if idx := strings.Index(e, "/"); idx != -1 {
		e = e[:idx]
	}

	parts := strings.SplitN(e, ":", 2)
	host = parts[0]
	if len(parts) == 2 {
		port = parts[1]
	} else {
		port = "4318"
	}
	return
}

func getEnvOrDefault(env, defaultValue string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return defaultValue
}
