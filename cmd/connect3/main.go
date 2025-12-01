package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Notes string `json:"notes"`
}

// Implement list interfaces
func (p Person) Title() string       { return p.Name }
func (p Person) Description() string { return p.Notes }
func (p Person) FilterValue() string { return p.Name }

type Relation struct {
	FromID      string `json:"from_id"`
	ToID        string `json:"to_id"`
	Strength    int    `json:"strength"` // 1-5
	Description string `json:"description"`
}

type Database struct {
	People    []Person   `json:"people"`
	Relations []Relation `json:"relations"`
}

const dbFileName = "data.json"

var (
	docStyle      = lipgloss.NewStyle().Margin(1, 2)
	titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	infoStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	strengthStyle = func(s int) string {
		switch s {
		case 1:
			return "⚪ Flüchtig (1)"
		case 2:
			return "🔵 Bekannt (2)"
		case 3:
			return "🟢 Gut Bekannt (3)"
		case 4:
			return "🟡 Freund (4)"
		case 5:
			return "🔴 Eng verbunden (5)"
		default:
			return "?"
		}
	}
)

type sessionState int

const (
	viewList sessionState = iota
	viewDetail
	viewCreatePerson
	viewCreateRelationSelectTarget
	viewCreateRelationDetails
)

type model struct {
	state    sessionState
	db       Database
	list     list.Model
	selected *Person // The currently viewed person

	inputName  textinput.Model
	inputNotes textarea.Model

	relTarget    *Person // Who should be connected
	inputRelDesc textinput.Model
	inputRelStr  textinput.Model // Input 1 to 5
}

func initialModel() model {
	db := loadData()

	items := make([]list.Item, len(db.People))
	for i, p := range db.People {
		items[i] = p
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Mein Privates CRM"
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "Neu")),
		}
	}

	ti := textinput.New()
	ti.Placeholder = "Name der Person"
	ti.Focus()

	ta := textarea.New()
	ta.Placeholder = "Notizen..."
	ta.SetHeight(3)

	tiRel := textinput.New()
	tiRel.Placeholder = "Beschreibung (z.B. Kennengelernt beim Sport)"

	tiStr := textinput.New()
	tiStr.Placeholder = "Stärke (1-5)"
	tiStr.CharLimit = 1

	return model{
		state:        viewList,
		db:           db,
		list:         l,
		inputName:    ti,
		inputNotes:   ta,
		inputRelDesc: tiRel,
		inputRelStr:  tiStr,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.state {
	case viewList:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "n" && !m.list.SettingFilter() {
				m.state = viewCreatePerson
				m.inputName.Focus()
				m.inputName.SetValue("")
				m.inputNotes.SetValue("")
				return m, nil
			}
			if msg.String() == "enter" {
				if i, ok := m.list.SelectedItem().(Person); ok {
					m.selected = &i
					m.state = viewDetail
				}
				return m, nil
			}
		case tea.WindowSizeMsg:
			h, v := docStyle.GetFrameSize()
			m.list.SetSize(msg.Width-h, msg.Height-v)
		}
		m.list, cmd = m.list.Update(msg)
		return m, cmd

	case viewDetail:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q":
				m.state = viewList
				m.selected = nil
				return m, nil
			case "r":
				m.state = viewCreateRelationSelectTarget
				m.list.Title = "Wähle eine Verbindung für " + m.selected.Name
				m.list.ResetSelected()
				return m, nil
			}
		}

	case viewCreatePerson:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.state = viewList
				return m, nil
			case "tab":
				if m.inputName.Focused() {
					m.inputName.Blur()
					m.inputNotes.Focus()
				} else {
					m.inputNotes.Blur()
					m.inputName.Focus()
				}
			case "enter":
				if m.inputName.Focused() {
					m.inputName.Blur()
					m.inputNotes.Focus()
					return m, nil
				}
				newP := Person{
					ID:    uuid.New().String(),
					Name:  m.inputName.Value(),
					Notes: m.inputNotes.Value(),
				}
				m.db.People = append(m.db.People, newP)
				saveData(m.db)
				cmd = m.list.SetItems(peopleToItems(m.db.People))
				m.state = viewList
				return m, cmd
			}
		}
		var cmdName, cmdNotes tea.Cmd
		m.inputName, cmdName = m.inputName.Update(msg)
		m.inputNotes, cmdNotes = m.inputNotes.Update(msg)
		return m, tea.Batch(cmdName, cmdNotes)

	case viewCreateRelationSelectTarget:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" {
				m.state = viewDetail
				m.list.Title = "Mein Privates CRM" // Reset Title
				return m, nil
			}
			if msg.String() == "enter" {
				if i, ok := m.list.SelectedItem().(Person); ok {
					if i.ID == m.selected.ID {
						return m, nil
					}
					m.relTarget = &i
					m.state = viewCreateRelationDetails
					m.inputRelStr.Focus()
					m.inputRelStr.SetValue("")
					m.inputRelDesc.SetValue("")
				}
				return m, nil
			}
		}
		m.list, cmd = m.list.Update(msg)
		return m, cmd

	case viewCreateRelationDetails:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.state = viewDetail
				return m, nil
			case "tab":
				if m.inputRelStr.Focused() {
					m.inputRelStr.Blur()
					m.inputRelDesc.Focus()
				} else {
					m.inputRelDesc.Blur()
					m.inputRelStr.Focus()
				}
			case "enter":
				if m.inputRelStr.Focused() {
					m.inputRelStr.Blur()
					m.inputRelDesc.Focus()
					return m, nil
				}
				strVal := 1
				fmt.Sscanf(m.inputRelStr.Value(), "%d", &strVal)
				if strVal < 1 {
					strVal = 1
				}
				if strVal > 5 {
					strVal = 5
				}

				newRel := Relation{
					FromID:      m.selected.ID,
					ToID:        m.relTarget.ID,
					Strength:    strVal,
					Description: m.inputRelDesc.Value(),
				}
				m.db.Relations = append(m.db.Relations, newRel)
				saveData(m.db)

				m.state = viewDetail
				m.list.Title = "Mein Privates CRM"
				return m, nil
			}
		}
		var cmdStr, cmdDesc tea.Cmd
		m.inputRelStr, cmdStr = m.inputRelStr.Update(msg)
		m.inputRelDesc, cmdDesc = m.inputRelDesc.Update(msg)
		return m, tea.Batch(cmdStr, cmdDesc)
	}

	return m, nil
}

func (m model) View() string {
	switch m.state {
	case viewList, viewCreateRelationSelectTarget:
		return docStyle.Render(m.list.View())

	case viewDetail:
		if m.selected == nil {
			return "Fehler: Keine Person ausgewählt"
		}
		s := titleStyle.Render(m.selected.Name) + "\n"
		s += infoStyle.Render(m.selected.Notes) + "\n\n"
		s += lipgloss.NewStyle().Underline(true).Render("Verbindungen:") + "\n"

		found := false
		for _, r := range m.db.Relations {
			if r.FromID == m.selected.ID || r.ToID == m.selected.ID {
				found = true
				otherID := r.ToID
				direction := "->"
				if r.ToID == m.selected.ID {
					otherID = r.FromID
					direction = "<-"
				}
				otherName := getName(m.db.People, otherID)

				line := fmt.Sprintf("%s %s %s [%s]",
					strengthStyle(r.Strength),
					direction,
					otherName,
					r.Description,
				)
				s += line + "\n"
			}
		}
		if !found {
			s += infoStyle.Render("Keine Verbindungen eingetragen.") + "\n"
		}

		s += "\n\n" + infoStyle.Render("ESC: Zurück | r: Neue Verbindung hinzufügen")
		return docStyle.Render(s)

	case viewCreatePerson:
		return docStyle.Render(fmt.Sprintf(
			"Neue Person anlegen\n\n%s\n\n%s\n\n%s",
			m.inputName.View(),
			m.inputNotes.View(),
			infoStyle.Render("Enter zum Speichern in Notizen"),
		))

	case viewCreateRelationDetails:
		return docStyle.Render(fmt.Sprintf(
			"Verbindung zu %s definieren\n\nStärke (1-5):\n%s\n\nBeschreibung:\n%s\n\n%s",
			m.relTarget.Name,
			m.inputRelStr.View(),
			m.inputRelDesc.View(),
			infoStyle.Render("Enter zum Speichern"),
		))
	}
	return ""
}

func loadData() Database {
	f, err := os.Open(dbFileName)
	if err != nil {
		return Database{People: []Person{}, Relations: []Relation{}}
	}
	defer f.Close()
	byteValue, _ := io.ReadAll(f)
	var db Database
	json.Unmarshal(byteValue, &db)
	return db
}

func saveData(db Database) {
	file, _ := json.MarshalIndent(db, "", " ")
	_ = os.WriteFile(dbFileName, file, 0644)
}

func peopleToItems(people []Person) []list.Item {
	items := make([]list.Item, len(people))
	for i, p := range people {
		items[i] = p
	}
	return items
}

func getName(people []Person, id string) string {
	for _, p := range people {
		if p.ID == id {
			return p.Name
		}
	}
	return "Unbekannt"
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Fehler: %v", err)
		os.Exit(1)
	}
}
