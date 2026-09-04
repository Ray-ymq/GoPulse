package routing

import "github.com/Ray-ymq/GoPulse/router/internal/config"

func Topic(messageType string) (string, bool) {
	if messageType == "metrics" || messageType == "logs" {
		return config.Topic, true
	}
	return "", false
}
