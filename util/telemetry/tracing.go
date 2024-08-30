package telemetry

import (
	"context"
	"os"
	//	"time"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"

	//	wfconfig "github.com/argoproj/argo-workflows/v3/config"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	//	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

type Tracing struct {
	//	config   *Config
	Tracer trace.Tracer
}

func NewTracing(ctx context.Context, serviceName string /*, config *Config*/) (*Tracing, error) {
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	)

	options := make([]tracesdk.TracerProviderOption, 0)
	options = append(options, tracesdk.WithResource(res))
	_, otlpEnabled := os.LookupEnv(`OTEL_EXPORTER_OTLP_ENDPOINT`)
	_, otlpTracingEnabled := os.LookupEnv(`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`)
	log.Info("Starting OTLP tracing stdout exporter")
	// stdoutExporter, err := stdouttrace.New(
	// 	stdouttrace.WithPrettyPrint())
	// if err != nil {
	// 	return nil, err
	// }
	// options = append(options, tracesdk.WithSyncer(stdoutExporter)) //, tracesdk.WithBatchTimeout(time.Second)))

	if otlpEnabled || otlpTracingEnabled {
		log.Info("Starting OTLP tracing GRPC exporter")
		otelExporter, err := otlptracegrpc.New(ctx) //, otlptracegrpc.WithTemporalitySelector(getTemporality(config)))
		if err != nil {
			return nil, err
		}
		options = append(options, tracesdk.WithSyncer(otelExporter)) //tracesdk.WithBatcher(otelExporter, tracesdk.WithBatchTimeout(time.Second)))
	}

	//	options = append(options, extraOpts...)
	// options = append(options, view(config))

	provider := tracesdk.NewTracerProvider(options...)
	otel.SetTracerProvider(provider)

	// // Add runtime metrics
	// err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	// if err != nil {
	// 	return nil, err
	// }

	tracer := provider.Tracer(serviceName)
	tracing := &Tracing{
		Tracer: tracer,
		// 		config:   config,
	}
	return tracing, nil
}
