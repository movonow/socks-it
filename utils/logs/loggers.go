package logs

import (
	"context"
	"flag"
	"github.com/tebeka/atexit"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
)

var alsoLogToStdout = flag.Bool("alsoLogToStdout", false, "whether to log to stdout")

var (
	once     sync.Once
	instance *slog.Logger
	levelVar = new(slog.LevelVar)
)

// GetLogger create a *slog.Logger instance, level: [Debug,Info,Warn,Error,Off]
func GetLogger(filename string, level string) *slog.Logger {
	// "Off" is a special case for testing, it's not a valid slog.Level
	if level == "Off" {
		return slog.New(nopHandler{})
	}

	// GetLogger maybe called multiple times during low level test.
	once.Do(func() {
		if err := levelVar.UnmarshalText([]byte(level)); err != nil {
			log.Fatal("set log level:", err)
		}

		// Set up lumberjack logger for log rotation
		fileLogger := &lumberjack.Logger{
			Filename:   filename, // Log file path
			MaxSize:    10,       // Maximum size in MB before rotating
			MaxBackups: 50,       // Maximum number of old logs to retain
			MaxAge:     30,       // Maximum number of days to retain old logs
			Compress:   true,     // Compress old log files
		}

		var sink io.Writer
		sink = fileLogger
		if *alsoLogToStdout {
			sink = io.MultiWriter(sink, os.Stdout)
		}

		handler := slog.NewTextHandler(sink, &slog.HandlerOptions{
			AddSource: true,
			Level:     levelVar,
			//ReplaceAttr: removeKeys(TimeKey),
		})

		atexit.Register(func() {
			_ = fileLogger.Close()
		})

		instance = slog.New(handler)
	})

	return instance
}

type nopHandler struct{}

func (n nopHandler) Enabled(context.Context, slog.Level) bool {
	return false
}

func (n nopHandler) Handle(context.Context, slog.Record) error {
	return nil
}

func (n nopHandler) WithAttrs([]slog.Attr) slog.Handler {
	return n
}

func (n nopHandler) WithGroup(string) slog.Handler {
	return n
}
