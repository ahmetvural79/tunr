package term

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Style wraps lipgloss.Style with Sprint/Printf/Println convenience methods
// so every cmd file doesn't have to juggle fmt.Print + Render by hand.
type Style struct {
	inner lipgloss.Style
}

func NewStyle(fg string) Style {
	return Style{inner: lipgloss.NewStyle().Foreground(lipgloss.Color(fg))}
}

func (s Style) Bold() Style      { return Style{inner: s.inner.Bold(true)} }
func (s Style) Underline() Style { return Style{inner: s.inner.Underline(true)} }
func (s Style) Italic() Style    { return Style{inner: s.inner.Italic(true)} }
func (s Style) Background(c string) Style {
	return Style{inner: s.inner.Background(lipgloss.Color(c))}
}

func (s Style) Sprint(a ...any) string {
	return s.inner.Render(fmt.Sprint(a...))
}

func (s Style) Sprintf(format string, a ...any) string {
	return s.inner.Render(fmt.Sprintf(format, a...))
}

func (s Style) Print(a ...any) {
	fmt.Print(s.Sprint(a...))
}

func (s Style) Println(a ...any) {
	fmt.Println(s.Sprint(a...))
}

func (s Style) Printf(format string, a ...any) {
	fmt.Print(s.Sprintf(format, a...))
}

func (s Style) Fprintln(w *os.File, a ...any) {
	fmt.Fprintln(w, s.Sprint(a...))
}

func (s Style) Fprintf(w *os.File, format string, a ...any) {
	fmt.Fprint(w, s.Sprintf(format, a...))
}

// Brand palette — tunr purple gradient territory
var (
	Green  = NewStyle("#22c55e").Bold()
	Red    = NewStyle("#ef4444").Bold()
	Yellow = NewStyle("#eab308").Bold()
	Cyan   = NewStyle("#22d3ee").Bold()
	Dim    = NewStyle("#6b7280")
	Bold   = Style{inner: lipgloss.NewStyle().Bold(true)}
	Purple = NewStyle("#a855f7").Bold()

	URL   = NewStyle("#22c55e").Bold().Underline()
	Faint = NewStyle("#525252")
)

// Semantic tokens — the glyphs that make terminal output feel alive
var (
	CheckMark = Green.Sprint("✓")
	CrossMark = Red.Sprint("✗")
	Arrow     = Dim.Sprint("→")
	Bullet    = Purple.Sprint("●")
)

func StyleForStatus(status int) Style {
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

// Box draws a bordered box around text — for those "look at me" moments
func Box(title, content string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#a855f7")).
		Padding(0, 2)

	header := Purple.Sprint(title)
	return box.Render(header + "\n" + content)
}

// Divider returns a styled horizontal rule
func Divider(width int) string {
	line := ""
	for i := 0; i < width; i++ {
		line += "─"
	}
	return Dim.Sprint(line)
}
