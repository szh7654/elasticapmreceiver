package elasticapmreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/collector/cmd/builder/elasticapmreceiver/translator"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/elastic/apm-data/input/elasticapm"
	"github.com/elastic/apm-data/model/modelpb"
)

type elasticapmReceiver struct {
	cfg        *Config
	serverHTTP *http.Server
	httpMux    *http.ServeMux
	shutdownWG sync.WaitGroup

	traceConsumer  consumer.Traces
	metricConsumer consumer.Metrics
	settings       receiver.Settings
}

func newElasticAPMReceiver(cfg *Config, settings receiver.Settings) *elasticapmReceiver {
	r := &elasticapmReceiver{
		cfg:      cfg,
		settings: settings,
		httpMux:  http.NewServeMux(),
	}
	if r.httpMux != nil {
		r.httpMux.HandleFunc(r.cfg.EventsURLPath, wrapper(r.handleEvents))
		r.httpMux.HandleFunc(r.cfg.RUMEventsUrlPath, wrapper(r.handleRUMEvents))
	}

	return r
}

func (r *elasticapmReceiver) startHTTPServer(cfg *confighttp.ServerConfig, host component.Host) error {

	r.settings.Logger.Info("Starting HTTP server", zap.String("endpoint", cfg.Endpoint))
	var hln net.Listener
	hln, err := cfg.ToListener(context.Background())
	if err != nil {
		return err
	}

	r.shutdownWG.Add(1)
	go func() {
		defer r.shutdownWG.Done()

		if errHTTP := r.serverHTTP.Serve(hln); errHTTP != http.ErrServerClosed {
			r.settings.Logger.Error("Error starting HTTP server", zap.Error(errHTTP))
		}
	}()
	return nil
}

func (r *elasticapmReceiver) Start(ctx context.Context, host component.Host) error {
	var err error
	r.serverHTTP, err = r.cfg.ServerConfig.ToServer(context.Background(),
		host,
		r.settings.TelemetrySettings,
		r.httpMux,
	)

	if err != nil {
		return err
	}

	err = r.startHTTPServer(r.cfg.ServerConfig, host)
	return err
}

func (r *elasticapmReceiver) Shutdown(ctx context.Context) error {
	var err error

	if r.serverHTTP != nil {
		err = r.serverHTTP.Shutdown(ctx)
	}

	r.shutdownWG.Wait()
	return err
}

func wrapper(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Only POST requests are supported")
			return
		}

		switch req.Header.Get("Content-Type") {
		// Only parse ndjson
		case "application/x-ndjson":
			f(w, req)
		default:
			writeError(w, http.StatusUnsupportedMediaType, "Only application/ndjson is supported")
			return
		}
	}
}

func (r *elasticapmReceiver) handleEvents(w http.ResponseWriter, req *http.Request) {
	traceData, metricData, err := r.handleTraces(w, req, &modelpb.APMEvent{})
	if err != nil {
		r.settings.Logger.Error("handleTraces error")
	}
	if traceData != nil && r.traceConsumer != nil {
		r.traceConsumer.ConsumeTraces(req.Context(), *traceData)
	}
	if metricData != nil && r.metricConsumer != nil {
		r.metricConsumer.ConsumeMetrics(req.Context(), *metricData)
	}
}

func (r *elasticapmReceiver) handleRUMEvents(w http.ResponseWriter, req *http.Request) {
	baseEvent := &modelpb.APMEvent{
		Timestamp: uint64(timestamppb.Now().Nanos),
	}
	traceData, metricData, _ := r.handleTraces(w, req, baseEvent)
	if traceData != nil {
		r.traceConsumer.ConsumeTraces(req.Context(), *traceData)
	}
	if metricData != nil {
		r.metricConsumer.ConsumeMetrics(req.Context(), *metricData)
	}
}

// Process a batch of events, returning the events and any error.
// The baseEvent is extended by the metadata in the batch and used as the base event for all events in the batch.
func (r *elasticapmReceiver) processBatch(reader io.Reader, baseEvent *modelpb.APMEvent) ([]*modelpb.APMEvent, error) {
	var events = []*modelpb.APMEvent{}
	processor := elasticapm.NewProcessor(elasticapm.Config{
		MaxEventSize: r.cfg.MaxEventSize,
		Semaphore:    semaphore.NewWeighted(1),
		Logger:       r.settings.Logger,
	})

	batchProcessor := modelpb.ProcessBatchFunc(func(ctx context.Context, batch *modelpb.Batch) error {
		events = append(events, (*batch)...)
		return nil
	})

	result := elasticapm.Result{}

	err := processor.HandleStream(
		context.Background(),
		baseEvent,
		reader,
		r.cfg.BatchSize,
		batchProcessor,
		&result,
	)

	if err != nil {
		return nil, err
	}

	return events, nil
}

func (r *elasticapmReceiver) handleTraces(w http.ResponseWriter, req *http.Request, baseEvent *modelpb.APMEvent) (*ptrace.Traces, *pmetric.Metrics, error) {
	events, err := r.processBatch(req.Body, baseEvent)

	if err != nil {
		writeError(w, http.StatusBadRequest, "Unable to decode events. Do you have valid ndjson?")
		return nil, nil, err
	}

	existTrace := false
	existMetric := false
	//existLog := false
	for _, event := range events {
		switch event.Type() {
		case modelpb.TransactionEventType:
			existTrace = true
		case modelpb.SpanEventType:
			existTrace = true
		case modelpb.MetricEventType:
			existMetric = true
			//case modelpb.LogEventType:
			//	existLog = true
		}

	}

	// 翻译resource
	resource := pcommon.NewResource()
	translator.ConvertMetadata(baseEvent, resource)
	// 初始化trace
	traceData := ptrace.NewTraces()

	resourceSpans := traceData.ResourceSpans().AppendEmpty()
	resource.CopyTo(resourceSpans.Resource())
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	spans := scopeSpans.Spans()

	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
	resource.CopyTo(resourceMetrics.Resource())
	scopeMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()
	ms := scopeMetrics.Metrics()

	for _, event := range events {
		bytes, _ := json.Marshal(event)
		fmt.Println("event: " + string(bytes))
		switch event.Type() {
		case modelpb.TransactionEventType:
			translator.ConvertTransaction(event, spans.AppendEmpty())
		case modelpb.SpanEventType:
			translator.ConvertSpan(event, spans.AppendEmpty())
		case modelpb.ErrorEventType:
			fmt.Println("Ignoring error")
		case modelpb.MetricEventType:
			translator.ConvertMetric(event, ms.AppendEmpty())

		case modelpb.LogEventType:
			fmt.Println("Ignoring log")
		default:
			fmt.Println("Unknown event type")
		}
	}
	resTrace := &traceData
	resMetric := &metrics
	if !existTrace {
		resTrace = nil
	}
	if !existMetric {
		resMetric = nil
	}
	return resTrace, resMetric, nil
}

func (r *elasticapmReceiver) registerMetricConsumer(metricConsumer consumer.Metrics) error {
	if metricConsumer != nil {
		return errors.New("metric consumer is nil")
	}
	r.metricConsumer = metricConsumer
	return nil
}

func (r *elasticapmReceiver) registerTraceConsumer(traceConsumer consumer.Traces) error {
	if traceConsumer == nil {
		return errors.New("trace consumer is nil")
	}

	r.traceConsumer = traceConsumer

	return nil
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("content-type", "text/plain")
	w.WriteHeader(statusCode)
	w.Write([]byte(message))
}
