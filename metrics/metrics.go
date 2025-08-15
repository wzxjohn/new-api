package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"strconv"
)

var (
	relaySuccess *prometheus.CounterVec
	relayFailure *prometheus.CounterVec
)

func InitMetrics() {
	relaySuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "success",
		Help:      "",
	}, []string{"origin_model", "final_model", "group", "channel_id"})
	relayFailure = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "failure",
		Help:      "",
	}, []string{"origin_model", "final_model", "group", "channel_id", "code"})
}

func ReportSuccess(originalModel, finalModel, group string, channelID int) {
	relaySuccess.WithLabelValues(originalModel, finalModel, group, strconv.Itoa(channelID)).Inc()
}

func ReportFailure(originalModel, finalModel, group string, channelID int, code int) {
	relayFailure.WithLabelValues(originalModel, finalModel, group, strconv.Itoa(channelID), strconv.Itoa(code)).Inc()
}
