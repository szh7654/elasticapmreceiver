package elasticapmreceiver

import (
	"context"
	"fmt"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

const (
	defaultHTTPEndpoint      = "0.0.0.0:8200"
	defaultEventsURLPath     = "/intake/v2/events"
	defaultRUMEventsURLPath  = "/intake/v2/rum/events"
	defaultMaxEventSizeBytes = 300 * 1024
	defaultBatchSize         = 10
)

// var receivers = sharedcomponent.NewSharedComponents()

type ConponentType struct {
}

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		Type,
		createDefaultConfig,
		//receiver.WithTraces(createTraces, component.StabilityLevelDevelopment),
		receiver.WithMetrics(createMetrics, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		ServerConfig: &confighttp.ServerConfig{
			Endpoint: defaultHTTPEndpoint,
		},
		EventsURLPath:    defaultEventsURLPath,
		RUMEventsUrlPath: defaultRUMEventsURLPath,
		MaxEventSize:     defaultMaxEventSizeBytes,
		BatchSize:        defaultBatchSize,
	}
}

//func createTraces(
//	_ context.Context,
//	settings receiver.Settings,
//	config component.Config,
//	trace consumer.Traces,
//) (receiver.Traces, error) {
//	fmt.Println("creating traces...")
//	if instance == nil {
//		cfg := config.(*Config)
//		newElasticAPMReceiver(cfg, settings)
//	}
//
//	instance.registerTraceConsumer(trace)
//	return instance, nil
//}

func createMetrics(_ context.Context, settings receiver.Settings, config component.Config, metrics consumer.Metrics) (receiver.Metrics, error) {
	fmt.Println("creating metrics...")
	cfg := config.(*Config)
	r := newElasticAPMReceiver(cfg, settings)
	r.metricConsumer = metrics
	r.registerMetricConsumer(metrics)
	return r, nil
}
