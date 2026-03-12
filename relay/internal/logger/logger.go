package logger

import (
	"fmt"
	"os"
	"time"
)

// Relay logger — sadece stderr'e yazar (stdout JSON-RPC için ayrıldı)
// Basit, hızlı, yapılandırılabilir log seviyeli.

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var currentLevel = INFO

func SetLevel(l Level) { currentLevel = l }

func log(level, prefix, format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s %s\n", ts, prefix, msg)
}

func Debug(format string, args ...interface{}) {
	if currentLevel <= DEBUG {
		log("DEBUG", "🔍", format, args...)
	}
}

func Info(format string, args ...interface{}) {
	if currentLevel <= INFO {
		log("INFO", "ℹ️ ", format, args...)
	}
}

func Warn(format string, args ...interface{}) {
	if currentLevel <= WARN {
		log("WARN", "⚠️ ", format, args...)
	}
}

func Fatal(format string, args ...interface{}) {
	log("FATAL", "💥", format, args...)
	os.Exit(1)
}
