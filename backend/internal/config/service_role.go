package config

import (
	"errors"
	"strings"
)

// ServiceRole selects the API and background responsibilities assembled by a
// Backend process. Combined is the backwards-compatible default used by the
// existing single-process deployment.
type ServiceRole string

const (
	ServiceRoleCombined ServiceRole = "combined"
	ServiceRoleBusiness ServiceRole = "business"
	ServiceRolePlatform ServiceRole = "platform"
)

func ParseServiceRole(raw string) (ServiceRole, error) {
	switch role := ServiceRole(strings.ToLower(strings.TrimSpace(raw))); role {
	case "", ServiceRoleCombined:
		return ServiceRoleCombined, nil
	case ServiceRoleBusiness:
		return ServiceRoleBusiness, nil
	case ServiceRolePlatform:
		return ServiceRolePlatform, nil
	default:
		return "", errors.New("BACKEND_SERVICE_ROLE must be one of combined, business, or platform")
	}
}

func LoadServiceRoleFrom(lookup LookupFunc) (ServiceRole, error) {
	if lookup == nil {
		return "", errors.New("configuration lookup is required")
	}
	value, ok := lookup("BACKEND_SERVICE_ROLE")
	if !ok {
		value = ""
	}
	return ParseServiceRole(value)
}

func (role ServiceRole) RunsBusiness() bool {
	return role == ServiceRoleBusiness || role == ServiceRoleCombined
}

func (role ServiceRole) RunsPlatform() bool {
	return role == ServiceRolePlatform || role == ServiceRoleCombined
}

func (role ServiceRole) IsValid() bool {
	return role == ServiceRoleBusiness || role == ServiceRolePlatform || role == ServiceRoleCombined
}
