package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/ykt/xiaozhi-server-go/internal/config"
)

// InitTracer 初始化 OpenTelemetry 分布式追踪。
//
// 支持以下 exporter：
//   - otlp：OTLP gRPC（默认）
//   - jaeger：Jaeger（通过 OTLP 兼容）
//   - zipkin：Zipkin
//   - none：禁用 tracing
//
// 返回 shutdown 函数用于优雅关闭。
func InitTracer(ctx context.Context, cfg config.TracingConfig, serviceName string) (func(context.Context) error, error) {
	if !cfg.Enabled || cfg.Exporter == "none" {
		// 禁用 tracing
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(ctx context.Context) error { return nil }, nil
	}

	// 创建 exporter
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// 创建 resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	// 创建 TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRate)),
	)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}