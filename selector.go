package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
)

type versionSelectModel struct {
	app       string
	versions  []string
	cursor    int
	current   string
	selected  string
	cancelled bool
}

func (m versionSelectModel) Init() tea.Cmd { return nil }

func (m versionSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.versions)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.versions[m.cursor]
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m versionSelectModel) View() string {
	s := fmt.Sprintf("Select version for %s:\n\n", m.app)
	for i, v := range m.versions {
		cursor := "  "
		if m.cursor == i {
			cursor = "> "
		}
		suffix := ""
		if v == m.current {
			suffix = " (current)"
		}
		s += fmt.Sprintf("%s%s%s\n", cursor, v, suffix)
	}
	s += "\n↑/↓: move  Enter: select  ESC: cancel\n"
	return s
}

func selectVersionInteractive(app string, versions []string, current string) (string, bool, error) {
	initialCursor := 0
	for i, v := range versions {
		if v == current {
			initialCursor = i
			break
		}
	}

	m := versionSelectModel{
		app:      app,
		versions: versions,
		cursor:   initialCursor,
		current:  current,
	}
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", false, err
	}

	result := finalModel.(versionSelectModel)
	return result.selected, result.cancelled, nil
}
