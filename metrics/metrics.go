package metrics

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"risk/risk"
)

const namespace = "risk"

// Labels never carry account or tx IDs; each would create a series per value.
type Metrics struct {
	registry *prometheus.Registry

	txsEvaluated     *prometheus.CounterVec
	rulesTriggered   *prometheus.CounterVec
	stageSeconds     *prometheus.HistogramVec
	txsSkipped       *prometheus.CounterVec
	settleFailures   *prometheus.CounterVec
	txsFailedThrough prometheus.Counter
	txsDeadLettered  prometheus.Counter
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		txsEvaluated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "txs_evaluated_total", Help: "Txs evaluated and published, by risk.",
		}, []string{"risk"}),
		rulesTriggered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "rule_triggered_total", Help: "Rule outcomes that triggered, by rule.",
		}, []string{"rule"}),
		stageSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "evaluation_stage_seconds", Help: "Time spent in each stage of evaluating a tx.",
			Buckets: []float64{0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"stage"}),
		txsSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "txs_skipped_total", Help: "Txs not evaluated again, by reason.",
		}, []string{"reason"}),
		settleFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "settle_failures_total", Help: "Failed attempts to settle a record, by kind.",
		}, []string{"kind"}),
		txsFailedThrough: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "txs_failed_through_total", Help: "Txs that couldn't be evaluated, published as UNKNOWN with the cause.",
		}),
		txsDeadLettered: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "txs_dead_lettered_total", Help: "Records with no tx id or account, sent only to the dead-letter topic.",
		}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.txsEvaluated, m.rulesTriggered, m.stageSeconds, m.txsSkipped,
		m.settleFailures, m.txsFailedThrough, m.txsDeadLettered,
	)

	for _, r := range []risk.Risk{risk.RiskUnknown, risk.RiskLow, risk.RiskMedium, risk.RiskHigh} {
		m.txsEvaluated.WithLabelValues(string(r))
	}
	return m
}

// A failed collector, such as an unreachable DB, drops only its own series from the scrape.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})
}

func (m *Metrics) WatchDB(db *sql.DB) {
	m.registry.MustRegister(collectors.NewDBStatsCollector(db, namespace))
}

func (m *Metrics) TxEvaluated(et risk.EvaluatedTx) {
	m.txsEvaluated.WithLabelValues(string(et.Risk)).Inc()
	for _, o := range et.RuleOutcomes {
		if o.Triggered {
			m.rulesTriggered.WithLabelValues(o.Rule).Inc()
		}
	}
}

func (m *Metrics) StageTook(stage string, d time.Duration) {
	m.stageSeconds.WithLabelValues(stage).Observe(d.Seconds())
}

func (m *Metrics) TxAlreadyEvaluated()      { m.txsSkipped.WithLabelValues("already_evaluated").Inc() }
func (m *Metrics) SettleFailed(kind string) { m.settleFailures.WithLabelValues(kind).Inc() }
func (m *Metrics) TxFailedThrough()         { m.txsFailedThrough.Inc() }
func (m *Metrics) TxDeadLettered()          { m.txsDeadLettered.Inc() }
