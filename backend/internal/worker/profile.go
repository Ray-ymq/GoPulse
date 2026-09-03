package worker

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

type Profile struct {
	Name             string
	Service          string
	Module           string
	ConsumerTag      string
	Topology         platform.Topology
	IgnoreSelfEvents bool
	IncludePostID    bool
}

var (
	BusinessProfile = Profile{
		Name: "business worker", Service: "business-worker", Module: "worker",
		ConsumerTag: "gopulse-business-worker", Topology: platform.BusinessTopology, IgnoreSelfEvents: true,
	}
	SearchProfile = Profile{
		Name: "search indexer", Service: "search-indexer", Module: "search",
		ConsumerTag: "gopulse-search-indexer", Topology: platform.SearchTopology, IncludePostID: true,
	}
)

func normalizeProfile(profile Profile) Profile {
	if profile.Name == "" {
		return BusinessProfile
	}
	return profile
}

func (profile Profile) allows(routingKey string) bool {
	for _, allowed := range profile.Topology.RoutingKeys {
		if routingKey == allowed {
			return true
		}
	}
	return false
}
