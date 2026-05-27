package ui

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ProgressEvent struct {
	Phase         string
	Language      string
	LanguageName  string
	LanguageIndex int
	LanguageTotal int
	Catalog       string
	CatalogIndex  int
	CatalogTotal  int
	BatchIndex    int
	BatchTotal    int
	Candidates    int
	Filled        int
	Copied        int
	Message       string
}

type Progress struct {
	enabled bool
	events  chan ProgressEvent
	program *tea.Program
}

func NewProgress(out io.Writer, enabled bool) *Progress {
	progress := &Progress{enabled: enabled}
	if !enabled {
		return progress
	}
	progress.events = make(chan ProgressEvent, 16)
	progress.program = tea.NewProgram(progressModel{events: progress.events}, tea.WithOutput(out))
	go func() {
		_, _ = progress.program.Run()
	}()
	return progress
}

func (p *Progress) Send(event ProgressEvent) {
	if p == nil || !p.enabled {
		return
	}
	p.events <- event
}

func (p *Progress) Close() {
	if p == nil || !p.enabled {
		return
	}
	close(p.events)
	if p.program != nil {
		p.program.Wait()
	}
}

type progressModel struct {
	events <-chan ProgressEvent
	event  ProgressEvent
	done   bool
}

type progressClosed struct{}

func (m progressModel) Init() tea.Cmd {
	return m.waitForEvent()
}

func (m progressModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case ProgressEvent:
		m.event = value
		return m, m.waitForEvent()
	case progressClosed:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m progressModel) View() string {
	if m.done || m.event.Phase == "" {
		return ""
	}
	var lines []string
	title := titleStyle.Render("strata")
	if m.event.Language != "" {
		title += " " + dimStyle.Render(fmt.Sprintf("language %d/%d", m.event.LanguageIndex, m.event.LanguageTotal))
	}
	lines = append(lines, title)

	if m.event.Language != "" {
		lines = append(lines, fmt.Sprintf("%s %s", okStyle.Render(m.event.Language), m.event.LanguageName))
	}
	if m.event.Catalog != "" {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("catalog %d/%d", m.event.CatalogIndex, m.event.CatalogTotal))+" "+filepath.Base(m.event.Catalog))
	}
	if m.event.BatchTotal > 0 {
		lines = append(lines, fmt.Sprintf("batch %d/%d  filled %d/%d  copied %d", m.event.BatchIndex, m.event.BatchTotal, m.event.Filled, m.event.Candidates, m.event.Copied))
	} else if m.event.Candidates > 0 {
		lines = append(lines, fmt.Sprintf("filled %d/%d  copied %d", m.event.Filled, m.event.Candidates, m.event.Copied))
	}
	if m.event.Message != "" {
		lines = append(lines, dimStyle.Render(m.event.Message))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...) + "\n"
}

func (m progressModel) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.events
		if !ok {
			return progressClosed{}
		}
		return event
	}
}

func ShortPath(path string) string {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) <= 3 {
		return clean
	}
	return filepath.Join(parts[len(parts)-3:]...)
}
