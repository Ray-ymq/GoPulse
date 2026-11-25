package config

import "errors"

// ValidateInstanceID 校验用于业务租约和消费者标识的实例名称。
func ValidateInstanceID(value string) error {
	if len(value) == 0 || len(value) > 64 || value[0] == '-' || value[len(value)-1] == '-' {
		return errors.New("实例名称长度必须为 1 至 64 字节，且不能以连字符开头或结尾")
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return errors.New("实例名称只能包含小写字母、数字和连字符")
		}
	}
	return nil
}
