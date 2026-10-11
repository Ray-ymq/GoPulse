package scenario

import (
	"fmt"
	"strings"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

const pluginAccountUsername = "gopulse_metrics"

// reconcilePluginAccounts provisions the least-privileged accounts used by
// the MySQL and RabbitMQ collectors. The command is idempotent for a fresh or
// retained acceptance project, and generated credentials stay in the session
// environment rather than in Compose command arguments or receipts.
func reconcilePluginAccounts(session *harness.Session) error {
	if session.Value("GOPULSE_PLUGIN_ACCOUNT_PASSWORD") == "" {
		return fmt.Errorf("plugin account password is unavailable")
	}

	mysqlScript := `set -eu
MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --protocol=socket --user=root --batch --skip-column-names --execute "CREATE USER IF NOT EXISTS 'gopulse_metrics'@'%' IDENTIFIED BY '$GOPULSE_PLUGIN_ACCOUNT_PASSWORD'; ALTER USER 'gopulse_metrics'@'%' IDENTIFIED BY '$GOPULSE_PLUGIN_ACCOUNT_PASSWORD'; GRANT USAGE ON *.* TO 'gopulse_metrics'@'%'; SELECT COUNT(*) FROM mysql.user WHERE User='gopulse_metrics' AND Host='%';"
`
	if result := session.Compose("exec", "-T", "-e", "GOPULSE_PLUGIN_ACCOUNT_PASSWORD", "mysql", "sh", "-ec", mysqlScript); result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "1" {
		return fmt.Errorf("MySQL plugin account reconciliation failed with exit code %d", result.ExitCode)
	}

	rabbitScript := `set -eu
if rabbitmqctl add_user gopulse_metrics "$GOPULSE_PLUGIN_ACCOUNT_PASSWORD" >/dev/null 2>&1; then :; else rabbitmqctl change_password gopulse_metrics "$GOPULSE_PLUGIN_ACCOUNT_PASSWORD" >/dev/null; fi
rabbitmqctl set_user_tags gopulse_metrics monitoring >/dev/null
rabbitmqctl set_permissions -p / gopulse_metrics '^$' '^$' '^$' >/dev/null
`
	if result := session.Compose("exec", "-T", "-e", "GOPULSE_PLUGIN_ACCOUNT_PASSWORD", "rabbitmq", "sh", "-ec", rabbitScript); result.ExitCode != 0 {
		return fmt.Errorf("RabbitMQ plugin account reconciliation failed with exit code %d", result.ExitCode)
	}
	return nil
}
