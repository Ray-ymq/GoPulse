package logger

import (
	"path/filepath"
	"std_exporter/common"
	"strings"

	"github.com/sirupsen/logrus"
)

// Logrus triggers HOOK when logging log-level messages returned by Levels()
type DefaultFieldHook struct {
	// logrus.AddHook(Hook) 添加hook
}

// Modify according to Fire method definition Logrus.Entry.
func (hook *DefaultFieldHook) Fire(entry *logrus.Entry) error {
	// 去除runtime跟踪的本地路径
	var rel string
	paths := strings.Split(entry.Caller.File, "/")
	for i, p := range paths {
		if p == common.NameSpace {
			rel = filepath.Join(paths[i:]...)
		}
	}
	if rel == "" {
		return nil
	}
	entry.Caller.File = rel
	return nil
}

func (hook *DefaultFieldHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func GetDefaultHook() logrus.Hook {
	return new(DefaultFieldHook)
}
