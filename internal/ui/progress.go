package ui

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
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
	cancel  context.CancelFunc
}

func NewProgress(ctx context.Context, out io.Writer, enabled bool) (*Progress, context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	view := &Progress{enabled: enabled, cancel: cancel}
	if !enabled {
		return view, runCtx
	}
	view.events = make(chan ProgressEvent, 16)
	view.program = tea.NewProgram(progressModel{
		events: view.events,
		cancel: cancel,
		bar: progress.New(
			progress.WithWidth(34),
			progress.WithSolidFill("#22C55E"),
		),
		width: 72,
	}, tea.WithOutput(out))
	go func() {
		_, _ = view.program.Run()
	}()
	return view, runCtx
}

func (p *Progress) Send(event ProgressEvent) {
	if p == nil || !p.enabled {
		return
	}
	p.events <- event
}

func (p *Progress) Cancel() {
	if p == nil || p.cancel == nil {
		return
	}
	p.cancel()
	if p.program != nil {
		p.program.Quit()
	}
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
	cancel context.CancelFunc
	event  ProgressEvent
	bar    progress.Model
	width  int
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
	case tea.WindowSizeMsg:
		m.width = maxInt(value.Width, 48)
		m.bar.Width = minInt(52, maxInt(24, m.width-24))
	case tea.KeyMsg:
		switch value.String() {
		case "ctrl+c", "q", "esc":
			if m.cancel != nil {
				m.cancel()
			}
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m progressModel) View() string {
	if m.done || m.event.Phase == "" {
		return ""
	}
	boxWidth := minInt(78, maxInt(48, m.width-4))
	rows := []string{
		lipgloss.JoinHorizontal(lipgloss.Center, titleStyle.Render("strata"), " ", statusPill(m.event.Message)),
		m.progressRow("Languages", m.languageProgress(), fmt.Sprintf("%d/%d", m.event.LanguageIndex, m.event.LanguageTotal)),
	}
	if m.event.CatalogTotal > 0 {
		rows = append(rows, m.progressRow("Catalogs", m.catalogProgress(), fmt.Sprintf("%d/%d", m.event.CatalogIndex, m.event.CatalogTotal)))
	}
	if m.event.BatchTotal > 0 {
		rows = append(rows, m.progressRow("Batches", m.batchProgress(), fmt.Sprintf("%d/%d", m.event.BatchIndex, m.event.BatchTotal)))
	}
	if m.event.Candidates > 0 {
		rows = append(rows, m.progressRow("Strings", ratio(m.event.Filled, m.event.Candidates), fmt.Sprintf("%d/%d", m.event.Filled, m.event.Candidates)))
	}
	rows = append(rows, "")
	rows = append(rows, labelValue("language", strings.TrimSpace(m.event.Language+" "+m.event.LanguageName)))
	if m.event.Catalog != "" {
		rows = append(rows, labelValue("catalog", ShortPath(m.event.Catalog)))
	}
	if m.event.Copied > 0 {
		rows = append(rows, labelValue("copied", fmt.Sprintf("%d non-linguistic strings", m.event.Copied)))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(1, 2).
		Width(boxWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...)) + "\n"
}

func (m progressModel) progressRow(label string, value float64, count string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		dimStyle.Width(10).Render(label),
		" ",
		m.bar.ViewAs(value),
		" ",
		okStyle.Width(8).Align(lipgloss.Right).Render(count),
	)
}

func (m progressModel) languageProgress() float64 {
	return ratio(maxInt(0, m.event.LanguageIndex-1), m.event.LanguageTotal)
}

func (m progressModel) catalogProgress() float64 {
	completed := maxInt(0, m.event.CatalogIndex-1)
	if m.event.Message == "catalog complete" || m.event.Message == "dry run complete" || m.event.Message == "up to date" {
		completed = m.event.CatalogIndex
	}
	return ratio(completed, m.event.CatalogTotal)
}

func (m progressModel) batchProgress() float64 {
	completed := maxInt(0, m.event.BatchIndex-1)
	if m.event.Message == "batch complete" || m.event.Message == "batch planned" {
		completed = m.event.BatchIndex
	}
	return ratio(completed, m.event.BatchTotal)
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

func statusPill(value string) string {
	if value == "" {
		value = "working"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("28")).
		Padding(0, 1).
		Render(value)
}

func labelValue(label, value string) string {
	if value == "" {
		value = "-"
	}
	return dimStyle.Width(10).Render(label) + " " + value
}

func ratio(value, total int) float64 {
	if total <= 0 {
		return 0
	}
	value = minInt(maxInt(value, 0), total)
	return float64(value) / float64(total)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
