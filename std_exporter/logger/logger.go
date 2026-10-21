package logger

import (
	"github.com/sirupsen/logrus"
	"log"
)

const (
	InfoLevel  = logrus.InfoLevel
	ErrorLevel = logrus.ErrorLevel
	DebugLevel = logrus.DebugLevel
)

type Logger struct {
	*logrus.Logger
}

var StdLogger *Logger

func initStdLogger() {
	logger := logrus.StandardLogger()
	logger.SetLevel(InfoLevel)
	logger.SetReportCaller(true)
	logger.SetFormatter(&logrus.TextFormatter{})
	StdLogger = &Logger{
		logger,
	}
	log.SetOutput(logger.Writer())
}

func GetStdLogger() *Logger {
	return StdLogger
}

func (l *Logger) SetLevel(level string) {
	switch level {
	case "info":
		l.Logger.SetLevel(InfoLevel)
	case "debug":
		l.Logger.SetLevel(DebugLevel)
	default:
		l.Logger.SetLevel(ErrorLevel)
	}
}

func init() {
	initStdLogger()
	std := GetStdLogger()
	std.AddHook(GetDefaultHook())
	//std.Out = os.Stdout
}
