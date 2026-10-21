package common

const (
	Source    = "kafka"
	PluginID  = "kafka-exporter"
	NameSpace = "gopulse_" + Source
)

var Version = "2.6.5"

const Topic = "gopulse-observability-metrics"
const Group = "gopulse-marshaller-metrics"
