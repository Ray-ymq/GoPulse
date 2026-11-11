package testenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	testcontainers "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/elasticsearch"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"

	"github.com/Ray-ymq/GoPulse/devtools/internal/envfile"
)

const (
	integrationMySQLImage         = "mysql:8.4.0"
	integrationRedisImage         = "redis:7.2.5-alpine"
	integrationRabbitMQImage      = "rabbitmq:3.13.3-management-alpine"
	integrationElasticsearchImage = "docker.elastic.co/elasticsearch/elasticsearch:9.5.2"
)

const (
	integrationMySQLUser      = "gopulse_integration"
	integrationMySQLPassword  = "integration-mysql"
	integrationMySQLDatabase  = "gopulse_integration"
	integrationRedisPassword  = "integration-redis"
	integrationRedisDatabase  = 15
	integrationRabbitUser     = "gopulse_integration"
	integrationRabbitPassword = "integration-rabbitmq"
)

type namedContainer struct {
	name      string
	container testcontainers.Container
}

// dependencyContainers owns one complete dependency set. It deliberately has
// no reuse or named volumes: a test run receives a new network and ephemeral
// container filesystem every time.
type dependencyContainers struct {
	network *testcontainers.DockerNetwork
	items   []namedContainer
	mysql   *mysql.MySQLContainer
}

func startDependencyContainers(ctx context.Context, runID string, observe bool) (*dependencyContainers, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TESTCONTAINERS_RYUK_DISABLED")), "true") {
		return nil, errors.New("resource reaper is disabled for Testcontainers; unset TESTCONTAINERS_RYUK_DISABLED")
	}
	// The Kafka module copies the Docker host and mapped port into its
	// advertised listener before the broker starts. A local Unix-socket daemon
	// can otherwise report 0.0.0.0, which Kafka rejects as non-routable.
	if strings.TrimSpace(os.Getenv("TESTCONTAINERS_HOST_OVERRIDE")) == "" {
		if err := os.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1"); err != nil {
			return nil, fmt.Errorf("set Testcontainers host override: %w", err)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	network, err := tcnetwork.New(ctx, tcnetwork.WithLabels(map[string]string{
		"gopulse.integration.run": runID,
	}))
	if err != nil {
		return nil, fmt.Errorf("create Testcontainers network: %w", err)
	}
	manager := &dependencyContainers{network: network}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = manager.terminate(context.Background())
		}
	}()

	labels := testcontainers.WithLabels(map[string]string{
		"gopulse.integration.run": runID,
	})

	manager.mysql, err = mysql.Run(ctx, integrationMySQLImage,
		mysql.WithDatabase(integrationMySQLDatabase),
		mysql.WithUsername(integrationMySQLUser),
		mysql.WithPassword(integrationMySQLPassword),
		labels,
		tcnetwork.WithNetwork([]string{"mysql"}, network),
	)
	if err != nil {
		return nil, fmt.Errorf("start MySQL container: %w", err)
	}
	manager.items = append(manager.items, namedContainer{name: "mysql", container: manager.mysql})
	if err := waitForExec(ctx, manager.mysql, "MySQL query", []string{
		"mysql", "-u" + integrationMySQLUser, "-p" + integrationMySQLPassword,
		"-D", integrationMySQLDatabase, "-e", "SELECT 1",
	}); err != nil {
		return nil, err
	}

	redis, err := rediscontainer.Run(ctx, integrationRedisImage,
		testcontainers.WithCmdArgs("--appendonly", "yes", "--requirepass", integrationRedisPassword, "--databases", "16"),
		labels,
		tcnetwork.WithNetwork([]string{"redis"}, network),
	)
	if err != nil {
		return nil, fmt.Errorf("start Redis container: %w", err)
	}
	manager.items = append(manager.items, namedContainer{name: "redis", container: redis})
	if err := waitForExec(ctx, redis, "Redis PING", []string{"redis-cli", "-a", integrationRedisPassword, "PING"}); err != nil {
		return nil, err
	}

	rabbit, err := rabbitmq.Run(ctx, integrationRabbitMQImage,
		rabbitmq.WithAdminUsername(integrationRabbitUser),
		rabbitmq.WithAdminPassword(integrationRabbitPassword),
		labels,
		tcnetwork.WithNetwork([]string{"rabbitmq"}, network),
	)
	if err != nil {
		return nil, fmt.Errorf("start RabbitMQ container: %w", err)
	}
	manager.items = append(manager.items, namedContainer{name: "rabbitmq", container: rabbit})
	if err := waitForExec(ctx, rabbit, "RabbitMQ protocol health", []string{"rabbitmq-diagnostics", "-q", "ping"}); err != nil {
		return nil, err
	}

	search, err := elasticsearch.Run(ctx, integrationElasticsearchImage,
		testcontainers.WithEnv(map[string]string{
			"discovery.type":                    "single-node",
			"xpack.security.enabled":            "false",
			"xpack.security.enrollment.enabled": "false",
			"ES_JAVA_OPTS":                      "-Xms512m -Xmx512m",
		}),
		labels,
		tcnetwork.WithNetwork([]string{"elasticsearch"}, network),
	)
	if err != nil {
		return nil, fmt.Errorf("start Elasticsearch container: %w", err)
	}
	manager.items = append(manager.items, namedContainer{name: "elasticsearch", container: search})

	return manager, nil

}

func (d *dependencyContainers) values(ctx context.Context, observe bool) (envfile.Values, error) {
	if d == nil || d.mysql == nil || d.network == nil {
		return nil, errors.New("dependencies for Testcontainers are not running")
	}
	host, err := d.mysql.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve Docker host: %w", err)
	}
	host = normalizeContainerHost(host)
	mysqlPort, err := mappedPort(ctx, d.mysql, "3306/tcp")
	if err != nil {
		return nil, err
	}
	values := envfile.Values{
		"INTEGRATION_RUN_ID":        d.network.Name,
		"INTEGRATION_DYNAMIC_PORTS": "1",
		"MYSQL_HOST":                host,
		"MYSQL_PORT":                strconv.Itoa(mysqlPort),
		"MYSQL_DATABASE":            integrationMySQLDatabase,
		"MYSQL_USER":                integrationMySQLUser,
		"MYSQL_PASSWORD":            integrationMySQLPassword,
		"MYSQL_ROOT_PASSWORD":       integrationMySQLPassword,
		"REDIS_HOST":                host,
		"REDIS_PASSWORD":            integrationRedisPassword,
		"REDIS_DB":                  strconv.Itoa(integrationRedisDatabase),
		"RABBITMQ_USER":             integrationRabbitUser,
		"RABBITMQ_PASSWORD":         integrationRabbitPassword,
	}
	redisPort, err := mappedPort(ctx, d.byName("redis"), "6379/tcp")
	if err != nil {
		return nil, err
	}
	values["REDIS_PORT"] = strconv.Itoa(redisPort)
	rabbitPort, err := mappedPort(ctx, d.byName("rabbitmq"), rabbitmq.DefaultAMQPPort)
	if err != nil {
		return nil, err
	}
	values["RABBITMQ_PORT"] = strconv.Itoa(rabbitPort)
	values["RABBITMQ_URL"] = fmt.Sprintf("amqp://%s:%s@%s:%d/", integrationRabbitUser, integrationRabbitPassword, host, rabbitPort)
	searchURL, err := endpoint(ctx, d.byName("elasticsearch"), "9200/tcp", "http")
	if err != nil {
		return nil, err
	}
	values["ELASTICSEARCH_URL"] = searchURL
	if searchPort, portErr := mappedPort(ctx, d.byName("elasticsearch"), "9200/tcp"); portErr == nil {
		values["ELASTICSEARCH_PORT"] = strconv.Itoa(searchPort)
	}
	if managementPort, portErr := mappedPort(ctx, d.byName("rabbitmq"), rabbitmq.DefaultHTTPPort); portErr == nil {
		values["RABBITMQ_MANAGEMENT_PORT"] = strconv.Itoa(managementPort)
	}

	values["REDIS_EXPORTER_HTTP_HOST"] = "127.0.0.1"
	for key, port := range reservePorts([]string{"HTTP_PORT"}) {
		values[key] = strconv.Itoa(port)
	}
	values["BUSINESS_STAGE_ENDPOINTS"] = "http://127.0.0.1:" + values["HTTP_PORT"]
	return values, nil
}

func (d *dependencyContainers) byName(name string) testcontainers.Container {
	for _, item := range d.items {
		if item.name == name {
			return item.container
		}
	}
	return nil
}

func (d *dependencyContainers) records(ctx context.Context) map[string]string {
	result := map[string]string{}
	for _, item := range d.items {
		if item.container != nil {
			result[item.name] = item.container.GetContainerID()
		}
	}
	return result
}

func (d *dependencyContainers) terminate(ctx context.Context) error {
	if d == nil {
		return nil
	}
	var cleanupErrors []error
	for index := len(d.items) - 1; index >= 0; index-- {
		item := d.items[index]
		if item.container == nil {
			continue
		}
		if err := item.container.Terminate(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("terminate %s container: %w", item.name, err))
		}
	}
	if d.network != nil {
		if err := d.network.Remove(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove Testcontainers network: %w", err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func (d *dependencyContainers) copyLogs(ctx context.Context, write func(string, string) error) error {
	if d == nil {
		return nil
	}
	var copyErrors []error
	for _, item := range d.items {
		if item.container == nil {
			continue
		}
		reader, err := item.container.Logs(ctx)
		if err != nil {
			copyErrors = append(copyErrors, fmt.Errorf("read %s container logs: %w", item.name, err))
			continue
		}
		encoded, readErr := io.ReadAll(io.LimitReader(reader, 2*1024*1024+1))
		closeErr := reader.Close()
		if readErr != nil {
			copyErrors = append(copyErrors, fmt.Errorf("read %s container logs: %w", item.name, readErr))
			continue
		}
		if closeErr != nil {
			copyErrors = append(copyErrors, fmt.Errorf("close %s container logs: %w", item.name, closeErr))
		}
		if err := write("container-"+item.name, string(encoded)); err != nil {
			copyErrors = append(copyErrors, err)
		}
	}
	return errors.Join(copyErrors...)
}

func mappedPort(ctx context.Context, container testcontainers.Container, port string) (int, error) {
	if container == nil {
		return 0, fmt.Errorf("container for port %s is missing", port)
	}
	mapped, err := container.MappedPort(ctx, port)
	if err != nil {
		return 0, fmt.Errorf("resolve mapped port %s: %w", port, err)
	}
	return int(mapped.Num()), nil
}

func endpoint(ctx context.Context, container testcontainers.Container, port, scheme string) (string, error) {
	if container == nil {
		return "", fmt.Errorf("container for endpoint %s is missing", port)
	}
	value, err := container.PortEndpoint(ctx, port, scheme)
	if err != nil {
		return "", fmt.Errorf("resolve %s endpoint: %w", port, err)
	}
	return value, nil
}

func waitForExec(parent context.Context, container testcontainers.Container, label string, command []string) error {
	ctx, cancel := context.WithTimeout(parent, readinessTimeout)
	defer cancel()
	var last string
	for {
		code, output, err := container.Exec(ctx, command)
		if err == nil && code == 0 {
			return nil
		}
		if output != nil {
			encoded, _ := io.ReadAll(io.LimitReader(output, 4096))
			last = strings.TrimSpace(string(encoded))
		}
		select {
		case <-ctx.Done():
			if last != "" {
				return fmt.Errorf("%s failed: %w (%s)", label, ctx.Err(), last)
			}
			return fmt.Errorf("%s failed: %w", label, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func normalizeContainerHost(host string) string {
	if host == "localhost" || host == "0.0.0.0" {
		return "127.0.0.1"
	}
	return host
}

func reservePorts(names []string) map[string]int {
	values := make(map[string]int, len(names))
	for _, name := range names {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		_, address, splitErr := net.SplitHostPort(listener.Addr().String())
		_ = listener.Close()
		if splitErr != nil {
			continue
		}
		port, parseErr := strconv.Atoi(address)
		if parseErr == nil && port > 0 {
			values[name] = port
		}
	}
	return values
}
