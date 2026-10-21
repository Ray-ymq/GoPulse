module github.com/Ray-ymq/GoPulse/perflab

go 1.26

require (
	github.com/Ray-ymq/GoPulse/exporters/mysql v0.0.0
	github.com/go-sql-driver/mysql v1.10.0
	golang.org/x/crypto v0.48.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.20.4 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.2 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/sys v0.41.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/Ray-ymq/GoPulse/exporters/mysql => ../exporters/mysql
