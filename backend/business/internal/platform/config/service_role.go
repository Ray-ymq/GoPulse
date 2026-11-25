package config

import (
	"errors"
	"strings"
)

// ServiceRole 标识业务 API 进程。
type ServiceRole string

const (
	ServiceRoleBusiness ServiceRole = "business"
)

func ParseServiceRole(raw string) (ServiceRole, error) {
	switch role := ServiceRole(strings.ToLower(strings.TrimSpace(raw))); role {
	case "", ServiceRoleBusiness:
		return ServiceRoleBusiness, nil
	default:
		return "", errors.New("BACKEND_SERVICE_ROLE 必须为 business")
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
	return role == ServiceRoleBusiness
}

func (role ServiceRole) IsValid() bool {
	return role == ServiceRoleBusiness
}
