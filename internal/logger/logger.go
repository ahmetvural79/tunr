package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
)

// LogLevel - hangi seviyelerde bağırıyoruz
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// Logger - ne log edileceğini ve ne zaman susulacağını bilen adam
type Logger struct {
	level  LogLevel
	prefix string
}

// tunr'nun varsayılan logger'ı - global çünkü hayat yeterince karmaşık
var defaultLogger = &Logger{level: INFO}

// renkli formatterlar - terminale biraz renk katalım, hayat zaten yeterince gri
var (
	debugStyle = color.New(color.FgHiBlack, color.Bold)
	infoStyle  = color.New(color.FgCyan, color.Bold)
	warnStyle  = color.New(color.FgYellow, color.Bold)
	errorStyle = color.New(color.FgRed, color.Bold)
	fatalStyle = color.New(color.FgHiRed, color.Bold, color.BgRed)
	timeStyle  = color.New(color.FgHiBlack)
	urlStyle   = color.New(color.FgGreen, color.Bold, color.Underline)
)

// New - yeni logger yarat, isteğe bağlı prefix ile
func New(prefix string) *Logger {
	return &Logger{level: INFO, prefix: prefix}
}

// SetLevel - log seviyesini ayarla (debug modu için)
func SetLevel(l LogLevel) {
	defaultLogger.level = l
}

// timestamp - güzel tarih formatı, çünkü Unix epoch kimse okuyamaz
func timestamp() string {
	return timeStyle.Sprint(time.Now().Format("15:04:05"))
}

// sanitize - KRITIK GÜVENLİK: token/secret gibi hassas değerlerin
// yanlışlıkla log'a geçmesini önlüyoruz. Açık kaynak = herkes okur.
func sanitize(msg string) string {
	// TODO: daha gelişmiş regex ile JWT, API key vb. maskele
	// Şimdilik basic string uzunluk kontrolü
	if len(msg) > 2000 {
		return msg[:2000] + "... [truncated for sanity]"
	}
	return msg
}

// Debug - "neden bu değer nil ki" dediğin anlarda
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

// Info - "her şey yolunda" anları için
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

// Warn - "bu olmamalıydı ama idare eder" durumları
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

// Error - "oh hayır" anları
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

// Fatal - "bu da mı olmadı, eve gidiyorum" anları
func Fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s %s\n",
		timestamp(),
		fatalStyle.Sprint("FATAL"),
		sanitize(msg),
	)
	os.Exit(1)
}

// PrintURL - başarılı tunnel URL'sini dev'e özenle göster
func PrintURL(tunnelURL string) {
	fmt.Println()
	fmt.Printf("  %s  %s\n",
		infoStyle.Sprint("🚀 Tunnel aktif:"),
		urlStyle.Sprint(tunnelURL),
	)
	fmt.Println()
}

// PrintBanner - tunr başladığında gösterilen baner
// (çünkü ASCII art her şeyi daha resmi gösterir)
func PrintBanner(version string) {
	banner := color.New(color.FgCyan, color.Bold)
	banner.Printf(`
  ██████╗ ██████╗ ███████╗██╗   ██╗ ██████╗
  ██╔══██╗██╔══██╗██╔════╝██║   ██║██╔═══██╗
  ██████╔╝██████╔╝█████╗  ██║   ██║██║   ██║
  ██╔═══╝ ██╔══██╗██╔══╝  ╚██╗ ██╔╝██║   ██║
  ██║     ██║  ██║███████╗ ╚████╔╝ ╚██████╔╝
  ╚═╝     ╚═╝  ╚═╝╚══════╝  ╚═══╝   ╚═════╝
`)
	color.New(color.FgHiBlack).Printf("  tunr.sh  •  v%s  •  local → public in < 3s\n\n", version)
}
