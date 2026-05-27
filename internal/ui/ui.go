package ui

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

type Printer struct {
	out   io.Writer
	err   io.Writer
	color bool
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

func New(out, err io.Writer) Printer {
	return Printer{out: out, err: err, color: isTerminal(out)}
}

func (p Printer) IsTerminal() bool {
	return p.color
}

func (p Printer) Output() io.Writer {
	return p.out
}

func (p Printer) Header(format string, args ...any) {
	p.printf(p.out, titleStyle, format+"\n", args...)
}

func (p Printer) Status(format string, args ...any) {
	p.printf(p.out, lipgloss.NewStyle(), "→ "+format+"\n", args...)
}

func (p Printer) Success(format string, args ...any) {
	p.printf(p.out, okStyle, "✓ "+format+"\n", args...)
}

func (p Printer) Warning(format string, args ...any) {
	p.printf(p.err, warnStyle, "• "+format+"\n", args...)
}

func (p Printer) Dim(format string, args ...any) {
	p.printf(p.out, dimStyle, format+"\n", args...)
}

func (p Printer) printf(writer io.Writer, style lipgloss.Style, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	if p.color {
		text = style.Render(text)
	}
	fmt.Fprint(writer, text)
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
