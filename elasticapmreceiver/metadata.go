package elasticapmreceiver

import "go.opentelemetry.io/collector/component"

var (
	Type      = component.MustNewType("elasticapm")
	ScopeName = "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/elasticapmreceiver"
)

const (
	MetricsStability = component.StabilityLevelBeta
)
