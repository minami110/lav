package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestVersionSelectModel_Navigation(t *testing.T) {
	m := versionSelectModel{
		app:      "testapp",
		versions: []string{"1.0.0", "2.0.0", "3.0.0"},
		cursor:   0,
		current:  "2.0.0",
	}

	// Down key moves cursor down
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	result := newModel.(versionSelectModel)
	if result.cursor != 1 {
		t.Errorf("down: expected cursor=1, got %d", result.cursor)
	}

	// Up key moves cursor up
	newModel, _ = result.Update(tea.KeyMsg{Type: tea.KeyUp})
	result = newModel.(versionSelectModel)
	if result.cursor != 0 {
		t.Errorf("up: expected cursor=0, got %d", result.cursor)
	}

	// Cursor doesn't go below 0
	newModel, _ = result.Update(tea.KeyMsg{Type: tea.KeyUp})
	result = newModel.(versionSelectModel)
	if result.cursor != 0 {
		t.Errorf("up at boundary: expected cursor=0, got %d", result.cursor)
	}

	// Cursor doesn't exceed max
	m.cursor = 2
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	result = newModel.(versionSelectModel)
	if result.cursor != 2 {
		t.Errorf("down at boundary: expected cursor=2, got %d", result.cursor)
	}
}

func TestVersionSelectModel_Selection(t *testing.T) {
	m := versionSelectModel{
		app:      "testapp",
		versions: []string{"1.0.0", "2.0.0"},
		cursor:   1,
	}

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	result := newModel.(versionSelectModel)

	if result.selected != "2.0.0" {
		t.Errorf("expected selected=2.0.0, got %s", result.selected)
	}
	if result.cancelled {
		t.Error("expected cancelled=false")
	}
	if cmd == nil {
		t.Error("expected non-nil command (tea.Quit)")
	}
}

func TestVersionSelectModel_Cancel(t *testing.T) {
	m := versionSelectModel{
		app:      "testapp",
		versions: []string{"1.0.0"},
		cursor:   0,
	}

	// ESC cancels
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	result := newModel.(versionSelectModel)
	if !result.cancelled {
		t.Error("esc: expected cancelled=true")
	}
	if cmd == nil {
		t.Error("esc: expected non-nil command")
	}

	// Ctrl+C cancels
	m.cancelled = false
	newModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	result = newModel.(versionSelectModel)
	if !result.cancelled {
		t.Error("ctrl+c: expected cancelled=true")
	}
}

func TestVersionSelectModel_View(t *testing.T) {
	m := versionSelectModel{
		app:      "myapp",
		versions: []string{"1.0.0", "2.0.0"},
		cursor:   0,
		current:  "1.0.0",
	}

	view := m.View()

	// View contains app name
	if !strings.Contains(view, "myapp") {
		t.Error("view should contain app name")
	}
	// View contains versions
	if !strings.Contains(view, "1.0.0") || !strings.Contains(view, "2.0.0") {
		t.Error("view should contain versions")
	}
	// View shows current marker
	if !strings.Contains(view, "(current)") {
		t.Error("view should mark current version")
	}
	// View shows cursor
	if !strings.Contains(view, ">") {
		t.Error("view should show cursor")
	}
}

func TestVersionSelectModel_Init(t *testing.T) {
	m := versionSelectModel{}
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init should return nil")
	}
}
