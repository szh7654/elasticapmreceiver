package translator

import (
	"fmt"
	"github.com/elastic/apm-data/model/modelpb"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func ConvertMetric(event *modelpb.APMEvent, dest pmetric.Metric) {
	if event == nil {
		return
	}
	for _, s := range event.GetMetricset().Samples {
		dest.SetName(s.Name)
		dest.SetUnit(s.Unit)
		switch s.Type {
		case modelpb.MetricType_METRIC_TYPE_UNSPECIFIED:
			gauge := dest.SetEmptyGauge()
			dp := gauge.DataPoints().AppendEmpty()
			dp.SetDoubleValue(s.Value)
			dp.SetTimestamp(pcommon.NewTimestampFromTime(ConvertNanoTime(event.Timestamp)))
		case modelpb.MetricType_METRIC_TYPE_GAUGE:
			gauge := dest.SetEmptyGauge()
			dp := gauge.DataPoints().AppendEmpty()
			dp.SetDoubleValue(s.Value)
			dp.SetTimestamp(pcommon.NewTimestampFromTime(ConvertNanoTime(event.Timestamp)))
		case modelpb.MetricType_METRIC_TYPE_COUNTER:
			counter := dest.SetEmptySum()
			counter.SetIsMonotonic(true)
			dp := counter.DataPoints().AppendEmpty()
			dp.SetDoubleValue(s.Value)
			dp.SetTimestamp(pcommon.NewTimestampFromTime(ConvertNanoTime(event.Timestamp)))
		case modelpb.MetricType_METRIC_TYPE_SUMMARY:
			summary := dest.SetEmptySummary()
			dp := summary.DataPoints().AppendEmpty()
			dp.SetCount(s.Summary.Count)
			dp.SetSum(s.Summary.Sum)
			dp.SetTimestamp(pcommon.NewTimestampFromTime(ConvertNanoTime(event.Timestamp)))
		case modelpb.MetricType_METRIC_TYPE_HISTOGRAM:
			histogram := dest.SetEmptyHistogram()
			dp := histogram.DataPoints().AppendEmpty()
			dp.SetCount(s.Summary.Count)
			dp.SetSum(s.Summary.Sum)
			dp.SetTimestamp(pcommon.NewTimestampFromTime(ConvertNanoTime(event.Timestamp)))
			for i, cnt := range s.Histogram.Counts {
				dp.BucketCounts().Append(cnt)
				dp.ExplicitBounds().Append(s.Histogram.Values[i])
			}
		default:
			fmt.Println("skipped undefined type")
		}
	}
}
