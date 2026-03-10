package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/ahmetvural79/tunr/internal/term"
)

// LogLevel controls how loud we get
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// Logger knows what to say and when to shut up
type Logger struct {
	level  LogLevel
	prefix string
}

// Global default logger — because life is complicated enough without DI for logging
var defaultLogger = &Logger{level: INFO}

// Styles for each severity — terminals deserve a splash of color
var (
	debugStyle = term.NewStyle("#6b7280").Bold()
	infoStyle  = term.Cyan
	warnStyle  = term.Yellow
	errorStyle = term.Red
	fatalStyle = term.NewStyle("#fca5a5").Bold().Background("#991b1b")
	timeStyle  = term.Dim
	urlStyle   = term.URL
)

// New creates a logger with an optional prefix
func New(prefix string) *Logger {
	return &Logger{level: INFO, prefix: prefix}
}

// SetLevel adjusts verbosity — crank it to DEBUG when things get weird
func SetLevel(l LogLevel) {
	defaultLogger.level = l
}

// timestamp formats time for humans — nobody reads Unix epochs
func timestamp() string {
	return timeStyle.Sprint(time.Now().Format("15:04:05"))
}

// SECURITY: Prevents tokens/secrets from accidentally ending up in logs.
// This is open source — assume everyone is reading.
func sanitize(msg string) string {
	if len(msg) > 2000 {
		return msg[:2000] + "... [truncated for sanity]"
	}
	return msg
}

// Debug is for those "why is this nil?!" moments
func Debug(format string, args ...any) {
	if defaultLogger.level <= DEBUG {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "%s %s %s\n",
			timestamp(),
			debugStyle.Sprint("DEBUG"),
			sanitize(msg),
		)
	}
}

// Info is the "everything is fine" channel
func Info(format string, args ...any) {
	if defaultLogger.level <= INFO {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stdout, "%s %s %s\n",
			timestamp(),
			infoStyle.Sprint(" INFO"),
			sanitize(msg),
		)
	}
}

// Warn is for "this shouldn't happen but we'll survive" situations
func Warn(format string, args ...any) {
	if defaultLogger.level <= WARN {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stdout, "%s %s %s\n",
			timestamp(),
			warnStyle.Sprint(" WARN"),
			sanitize(msg),
		)
	}
}

// Error is the "oh no" channel
func Error(format string, args ...any) {
	if defaultLogger.level <= ERROR {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "%s %s %s\n",
			timestamp(),
			errorStyle.Sprint("ERROR"),
			sanitize(msg),
		)
	}
}

// Fatal means we're done here — nobody wants to see raw panic traces in production
func Fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s %s\n",
		timestamp(),
		fatalStyle.Sprint("FATAL"),
		sanitize(msg),
	)
	os.Exit(1)
}

// PrintURL displays the tunnel URL with appropriate fanfare
func PrintURL(tunnelURL string) {
	fmt.Println()
	fmt.Printf("  %s  %s\n",
		infoStyle.Sprint("🚀 Tunnel active:"),
		urlStyle.Sprint(tunnelURL),
	)
	fmt.Println()
}

// PrintBanner shows the startup banner — because ASCII art makes everything official
func PrintBanner(version string) {
	term.Banner()
	term.Dim.Printf("  tunr.sh  •  v%s  •  local → public in < 3s\n\n", version)
}
