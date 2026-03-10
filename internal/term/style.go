package term

import (
	"fmt"

	"github.com/fatih/color"
)

var (
	Green  = color.New(color.FgGreen, color.Bold)
	Red    = color.New(color.FgRed, color.Bold)
	Yellow = color.New(color.FgYellow, color.Bold)
	Cyan   = color.New(color.FgCyan, color.Bold)
	Dim    = color.New(color.FgHiBlack)
	Bold   = color.New(color.Bold)
	Purple = color.New(color.FgMagenta, color.Bold)
)

var (
	CheckMark = Green.Sprint("✓")
	CrossMark = Red.Sprint("✗")
	Arrow     = Dim.Sprint("→")
	Bullet    = Purple.Sprint("●")
)

func StyleForStatus(status int) *color.Color {
	switch {
	case status >= 500:
		return Red
	case status >= 400:
		return Yellow
	case status >= 300:
		return Cyan
	default:
		return Green
	}
}

type Step struct {
	Name string
	Fn   func() (string, error)
}

func RunSteps(steps []Step) error {
	for _, s := range steps {
		Dim.Printf("  %s...", s.Name)
		result, err := s.Fn()
		if err != nil {
			Red.Println(" failed")
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		if result != "" {
			Green.Printf(" %s\n", result)
		} else {
			Green.Println(" done")
		}
	}
	return nil
}

func FormatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func Banner() {
	Purple.Println()
	Purple.Println("  ████████╗██╗   ██╗███╗   ██╗██████╗ ")
	Purple.Println("  ╚══██╔══╝██║   ██║████╗  ██║██╔══██╗")
	Purple.Println("     ██║   ██║   ██║██╔██╗ ██║██████╔╝")
	Purple.Println("     ██║   ██║   ██║██║╚██╗██║██╔══██╗")
	Purple.Println("     ██║   ╚██████╔╝██║ ╚████║██║  ██║")
	Purple.Println("     ╚═╝    ╚═════╝ ╚═╝  ╚═══╝╚═╝  ╚═╝")
	Dim.Println("     Local → Public in < 3 seconds")
	fmt.Println()
}
