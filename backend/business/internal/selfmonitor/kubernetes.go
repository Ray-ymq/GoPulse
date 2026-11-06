package selfmonitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type cluster struct {
	client                 *http.Client
	base, namespace, token string
}

type workload struct{ resource, name string }

func openCluster() (*cluster, error) {
	const directory = "/var/run/secrets/kubernetes.io/serviceaccount/"
	namespacePath, tokenPath, caPath, base := directory+"namespace", directory+"token", directory+"ca.crt", "https://kubernetes.default.svc"

	namespace, err := os.ReadFile(namespacePath)
	if err != nil {
		return nil, err
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid cluster CA")
	}
	ns := strings.TrimSpace(string(namespace))
	if expected := os.Getenv("POD_NAMESPACE"); expected != "" && expected != ns {
		return nil, errors.New("当前命名空间与 ServiceAccount 不一致")
	}
	if ns != "gopulse-business" && ns != "gopulse-observability" && ns != "gopulse-all" {
		return nil, errors.New("unsupported namespace")
	}
	return &cluster{client: &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}}, base: base, namespace: ns, token: strings.TrimSpace(string(token))}, nil
}

func (k *cluster) workloads() []workload {
	rows := []workload{{"statefulsets", "selfmonitor-prometheus"}, {"statefulsets", "selfmonitor-grafana"}, {"daemonsets", "selfmonitor-node"}, {"deployments", "selfmonitor-mysql-exporter"}}
	if k.namespace != "gopulse-observability" {
		rows = append(rows, workload{"deployments", "selfmonitor-redis-exporter"}, workload{"deployments", "selfmonitor-elasticsearch-exporter"})
	}
	if k.namespace != "gopulse-business" {
		rows = append(rows, workload{"deployments", "selfmonitor-kafka-exporter"}, workload{"deployments", "selfmonitor-observability-elasticsearch-exporter"})
	}
	return rows
}

func (k *cluster) request(ctx context.Context, method, path string, body any, out any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.base+"/apis/apps/v1/namespaces/"+k.namespace+"/"+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+k.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	}
	res, err := k.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("cluster control unavailable")
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	return nil
}

func (k *cluster) state(ctx context.Context) (Status, error) {
	stopped, stopping, starting, disabled := true, false, false, false
	for _, row := range k.workloads() {
		var value struct {
			Spec struct {
				Replicas int `json:"replicas"`
				Template struct {
					Spec struct {
						NodeSelector map[string]string `json:"nodeSelector"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
			Status struct{ Replicas, ReadyReplicas, NumberReady, CurrentNumberScheduled int } `json:"status"`
		}
		if err := k.request(ctx, http.MethodGet, row.resource+"/"+row.name, nil, &value); err != nil {
			return status("unavailable", "control_unavailable"), err
		}
		desired, ready, actual := value.Spec.Replicas, value.Status.ReadyReplicas, value.Status.Replicas
		if row.resource == "daemonsets" {
			desired, ready, actual = 1, value.Status.NumberReady, value.Status.CurrentNumberScheduled
			if value.Spec.Template.Spec.NodeSelector["gopulse.io/selfmonitor-disabled"] == "true" {
				desired = 0
			}
		}
		if desired > 0 {
			stopped = false
			if ready < desired {
				starting = true
			}
		} else {
			disabled = true
			if actual > 0 {
				stopping = true
			}
		}
	}
	if stopping {
		return status("stopping", "stopping"), nil
	}
	if stopped {
		return status("stopped", "stopped"), nil
	}
	if disabled {
		return status("failed", "control_incomplete"), nil
	}
	if starting {
		return status("starting", "starting"), nil
	}
	return status("running", "ok"), nil
}

func (k *cluster) change(ctx context.Context, action string) error {
	replicas := 0
	if action == "start" {
		replicas = 1
	}
	for _, row := range k.workloads() {
		path := row.resource + "/" + row.name + "/scale"
		body := map[string]any{"spec": map[string]any{"replicas": replicas}}
		if row.resource == "daemonsets" {
			path = row.resource + "/" + row.name
			var disabled any
			if replicas == 0 {
				disabled = "true"
			}
			body = map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"nodeSelector": map[string]any{"gopulse.io/selfmonitor-disabled": disabled}}}}}
		}
		if err := k.request(ctx, http.MethodPatch, path, body, nil); err != nil {
			return err
		}
	}
	return nil
}

func (k *cluster) expectedTargets() map[string]int {
	expected := map[string]int{"backend": 1, "platform-api": 1, "business-worker": 1, "search-indexer": 1, "monitor": 1, "marshaller": 1, "node": 1, "mysql": 1, "redis": 1, "rabbitmq": 1, "kafka": 1, "elasticsearch": 1, "observability-elasticsearch": 1, "victoriametrics": 1, "prometheus": 1, "kubernetes-cadvisor": 1}
	if k.namespace == "gopulse-business" {
		for _, job := range []string{"monitor", "marshaller", "kafka", "observability-elasticsearch", "victoriametrics"} {
			delete(expected, job)
		}
	}
	if k.namespace == "gopulse-observability" {
		for _, job := range []string{"backend", "business-worker", "search-indexer", "redis", "rabbitmq", "elasticsearch"} {
			delete(expected, job)
		}
	}
	return expected
}
func (k *cluster) sourceQueries() []string {
	queries := []string{`mysql_up{job="mysql"}`}
	if k.namespace != "gopulse-observability" {
		queries = append(queries, `redis_up{job="redis"}`, `elasticsearch_cluster_health_number_of_nodes{job="elasticsearch"}`)
	}
	if k.namespace != "gopulse-business" {
		queries = append(queries, `elasticsearch_cluster_health_number_of_nodes{job="observability-elasticsearch"}`)
	}
	return queries
}
