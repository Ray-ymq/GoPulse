module github.com/Ray-ymq/GoPulse/monitor

go 1.26

replace github.com/Ray-ymq/GoPulse/componentmetrics => ../componentmetrics

require (
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.67.2
	github.com/twmb/franz-go v1.21.0
	go.etcd.io/bbolt v1.5.0
	golang.org/x/sys v0.45.0
)

require github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect

require (
	github.com/Ray-ymq/GoPulse/componentmetrics v0.1.0
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
