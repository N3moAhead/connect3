package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/N3moAhead/connect3/internal/config"
	"github.com/N3moAhead/connect3/internal/db"
	"github.com/N3moAhead/connect3/internal/person"
	"github.com/N3moAhead/connect3/internal/relation"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// --- STYLING ---

var (
	docStyle   = lipgloss.NewStyle().Margin(1, 2)
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Red
)

// --- APPLICATION STATES ---

type sessionState int

const (
	viewListPeople sessionState = iota
	viewDetail
	viewPersonForm     // Used for Create and Edit
	viewRelationTarget // Select who to connect to
	viewRelationForm   // Used for Create and Edit
	viewConfirmDeletePerson
	viewConfirmDeleteRelation
)

// --- MAIN MODEL ---

type model struct {
	state  sessionState
	db     db.Database
	dbPath string

	// Lists
	listPeople    list.Model
	listRelations list.Model // Embedded in Detail View

	// Selections
	selectedPerson *person.Person
	selectedRel    *relation.Relation
	targetPerson   *person.Person // For creating new relations

	// Forms
	isEditing    bool // Are we creating or editing?
	inputName    textinput.Model
	inputNotes   textarea.Model
	inputRelDesc textinput.Model
	inputRelStr  textinput.Model
}

func getDefaultDBPath() string {
	// Try XDG_DATA_HOME
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData != "" {
		return filepath.Join(xdgData, "connect3", config.DB_FILE_NAME)
	}

	// Fallback to ~/.local/share
	home, err := os.UserHomeDir()
	if err != nil {
		// Last resort fallback
		return "data.json"
	}
	return filepath.Join(home, ".local", "share", "connect3", config.DB_FILE_NAME)
}

func initialModel(dbPath string) model {
	database := loadData(dbPath)

	// 1. Init People List
	items := make([]list.Item, len(database.People))
	for i, p := range database.People {
		items[i] = p
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Connect3"
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "New Person")),
		}
	}

	// 2. Init Relation List (Initially empty)
	lr := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	lr.Title = "Connections"
	lr.SetShowTitle(false)
	lr.SetFilteringEnabled(false) // Disable filter in detail view to keep it clean
	lr.SetShowHelp(false)         // We show custom help in view
	lr.SetShowStatusBar(false)    // Minimalist look
	lr.DisableQuitKeybindings()

	// 3. Init Inputs
	ti := textinput.New()
	ti.Placeholder = "Full Name"

	ta := textarea.New()
	ta.Placeholder = "Notes..."
	ta.SetHeight(3)

	tiRelDesc := textinput.New()
	tiRelDesc.Placeholder = "Description (e.g. Work Colleague)"

	tiRelStr := textinput.New()
	tiRelStr.Placeholder = "Strength (1-5)"
	tiRelStr.CharLimit = 1

	return model{
		state:         viewListPeople,
		db:            database,
		dbPath:        dbPath,
		listPeople:    l,
		listRelations: lr,
		inputName:     ti,
		inputNotes:    ta,
		inputRelDesc:  tiRelDesc,
		inputRelStr:   tiRelStr,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

// --- UPDATE ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// CRITICAL FIX: Update sizes for BOTH lists regardless of current view
		h, v := docStyle.GetFrameSize()
		m.listPeople.SetSize(msg.Width-h, msg.Height-v)

		// Relations list is smaller (subtract header space approx 10 lines)
		relHeight := max(msg.Height-v-12, 5)
		m.listRelations.SetSize(msg.Width-h, relHeight)
	}

	switch m.state {

	// ---------------------------------------------------------
	// 1. MAIN PEOPLE LIST
	// ---------------------------------------------------------
	case viewListPeople:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "n":
				m.state = viewPersonForm
				m.isEditing = false
				m.inputName.SetValue("")
				m.inputNotes.SetValue("")
				m.inputName.Focus()
				m.inputNotes.Blur()
				return m, nil
			case "enter":
				if i, ok := m.listPeople.SelectedItem().(person.Person); ok {
					m.selectedPerson = &i
					m.state = viewDetail
					// Refresh relation list items
					m.refreshRelationList()
				}
				return m, nil
			}
		}
		m.listPeople, cmd = m.listPeople.Update(msg)
		return m, cmd

	// ---------------------------------------------------------
	// 2. DETAIL VIEW (Contains Info + Relations List)
	// ---------------------------------------------------------
	case viewDetail:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			// Global Detail Keys
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewListPeople
				m.selectedPerson = nil
				return m, nil
			case "E": // Shift+e to edit Person
				m.state = viewPersonForm
				m.isEditing = true
				m.inputName.SetValue(m.selectedPerson.Name)
				m.inputNotes.SetValue(m.selectedPerson.Notes)
				m.inputName.Focus()
				m.inputNotes.Blur()
				return m, nil
			case "D": // Shift+d to delete Person
				m.state = viewConfirmDeletePerson
				return m, nil

			// Relation List Keys
			case "n":
				// Add new relation
				m.state = viewRelationTarget
				m.listPeople.Title = "Select person to connect with " + m.selectedPerson.Name
				m.listPeople.ResetSelected()
				return m, nil
			case "e":
				// Edit selected relation
				if len(m.listRelations.Items()) > 0 {
					if i, ok := m.listRelations.SelectedItem().(relation.RelationItem); ok {
						m.selectedRel = &i.Rel
						m.state = viewRelationForm
						m.isEditing = true
						m.inputRelDesc.SetValue(i.Rel.Description)
						m.inputRelStr.SetValue(fmt.Sprintf("%d", i.Rel.Strength))
						m.inputRelStr.Focus()
						m.inputRelDesc.Blur()
					}
				}
				return m, nil
			case "d":
				// Delete selected relation
				if len(m.listRelations.Items()) > 0 {
					if i, ok := m.listRelations.SelectedItem().(relation.RelationItem); ok {
						m.selectedRel = &i.Rel
						m.state = viewConfirmDeleteRelation
					}
				}
				return m, nil
			}
		}
		// Forward keys to the relations list (navigation)
		m.listRelations, cmd = m.listRelations.Update(msg)
		return m, cmd

	// ---------------------------------------------------------
	// 3. PERSON FORM (Create / Edit)
	// ---------------------------------------------------------
	case viewPersonForm:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				if m.isEditing {
					m.state = viewDetail
				} else {
					m.state = viewListPeople
				}
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
				// Save Logic
				if m.isEditing {
					// Update existing
					for i, p := range m.db.People {
						if p.ID == m.selectedPerson.ID {
							m.db.People[i].Name = m.inputName.Value()
							m.db.People[i].Notes = m.inputNotes.Value()
							m.selectedPerson = &m.db.People[i] // Update pointer
							break
						}
					}
				} else {
					// Create new
					newP := person.Person{
						ID:    uuid.New().String(),
						Name:  m.inputName.Value(),
						Notes: m.inputNotes.Value(),
					}
					m.db.People = append(m.db.People, newP)
				}
				saveData(m.db, m.dbPath)
				m.listPeople.SetItems(peopleToItems(m.db.People))

				if m.isEditing {
					m.state = viewDetail
				} else {
					m.state = viewListPeople
				}
				return m, nil
			}
		}
		var cmdName, cmdNotes tea.Cmd
		m.inputName, cmdName = m.inputName.Update(msg)
		m.inputNotes, cmdNotes = m.inputNotes.Update(msg)
		return m, tea.Batch(cmdName, cmdNotes)

	// ---------------------------------------------------------
	// 4. CONFIRM DELETE PERSON
	// ---------------------------------------------------------
	case viewConfirmDeletePerson:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "y" || msg.String() == "Y" {
				// 1. Delete relations
				newRels := []relation.Relation{}
				for _, r := range m.db.Relations {
					if r.FromID != m.selectedPerson.ID && r.ToID != m.selectedPerson.ID {
						newRels = append(newRels, r)
					}
				}
				m.db.Relations = newRels

				// 2. Delete person
				newPeople := []person.Person{}
				for _, p := range m.db.People {
					if p.ID != m.selectedPerson.ID {
						newPeople = append(newPeople, p)
					}
				}
				m.db.People = newPeople

				saveData(m.db, m.dbPath)
				m.listPeople.SetItems(peopleToItems(m.db.People))
				m.state = viewListPeople
				m.selectedPerson = nil
			} else if msg.String() == "n" || msg.String() == "N" || msg.String() == "esc" {
				m.state = viewDetail
			}
		}

	// ---------------------------------------------------------
	// 5. RELATION TARGET SELECT
	// ---------------------------------------------------------
	case viewRelationTarget:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" {
				m.state = viewDetail
				m.listPeople.Title = "People" // Reset title
				return m, nil
			}
			if msg.String() == "enter" {
				if i, ok := m.listPeople.SelectedItem().(person.Person); ok {
					if i.ID == m.selectedPerson.ID {
						// Can't link to self
						return m, nil
					}
					m.targetPerson = &i
					m.state = viewRelationForm
					m.isEditing = false
					m.inputRelStr.Focus()
					m.inputRelDesc.Blur()
					m.inputRelStr.SetValue("")
					m.inputRelDesc.SetValue("")
				}
				return m, nil
			}
		}
		m.listPeople, cmd = m.listPeople.Update(msg)
		return m, cmd

	// ---------------------------------------------------------
	// 6. RELATION FORM
	// ---------------------------------------------------------
	case viewRelationForm:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.state = viewDetail
				m.listPeople.Title = "People"
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

				// Validate Strength
				strVal, _ := strconv.Atoi(m.inputRelStr.Value())
				if strVal < 1 {
					strVal = 1
				}
				if strVal > 5 {
					strVal = 5
				}

				if m.isEditing {
					// Update existing relation
					for i, r := range m.db.Relations {
						if r.ID == m.selectedRel.ID {
							m.db.Relations[i].Strength = strVal
							m.db.Relations[i].Description = m.inputRelDesc.Value()
							break
						}
					}
				} else {
					// Create new relation
					newRel := relation.Relation{
						ID:          uuid.New().String(),
						FromID:      m.selectedPerson.ID,
						ToID:        m.targetPerson.ID,
						Strength:    strVal,
						Description: m.inputRelDesc.Value(),
					}
					m.db.Relations = append(m.db.Relations, newRel)
				}
				saveData(m.db, m.dbPath)
				m.refreshRelationList()
				m.state = viewDetail
				m.listPeople.Title = "People" // Reset Title if we came from target select
				return m, nil
			}
		}
		var cmdStr, cmdDesc tea.Cmd
		m.inputRelStr, cmdStr = m.inputRelStr.Update(msg)
		m.inputRelDesc, cmdDesc = m.inputRelDesc.Update(msg)
		return m, tea.Batch(cmdStr, cmdDesc)

	// ---------------------------------------------------------
	// 7. CONFIRM DELETE RELATION
	// ---------------------------------------------------------
	case viewConfirmDeleteRelation:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "y" || msg.String() == "Y" {
				newRels := []relation.Relation{}
				for _, r := range m.db.Relations {
					if r.ID != m.selectedRel.ID {
						newRels = append(newRels, r)
					}
				}
				m.db.Relations = newRels
				saveData(m.db, m.dbPath)
				m.refreshRelationList()
				m.state = viewDetail
			} else if msg.String() == "n" || msg.String() == "N" || msg.String() == "esc" {
				m.state = viewDetail
			}
		}
	}

	return m, nil
}

// --- VIEW ---

func (m model) View() string {
	switch m.state {
	case viewListPeople:
		return docStyle.Render(m.listPeople.View())

	case viewRelationTarget:
		return docStyle.Render(m.listPeople.View())

	case viewDetail:
		if m.selectedPerson == nil {
			return "Error: No person selected."
		}
		// Header Info
		s := titleStyle.Render(m.selectedPerson.Name) + "\n"
		s += infoStyle.Render(m.selectedPerson.Notes) + "\n\n"

		// Commands Help
		help := infoStyle.Render("E: Edit Person | D: Delete Person | n: New Rel | e: Edit Rel | d: Del Rel | ESC: Back")

		s += help + "\n\n"
		s += lipgloss.NewStyle().Underline(true).Render("Connections:") + "\n"

		// The list view handles the rendering of the items
		s += m.listRelations.View()
		return docStyle.Render(s)

	case viewPersonForm:
		title := "Create New Person"
		if m.isEditing {
			title = "Edit Person"
		}

		return docStyle.Render(fmt.Sprintf(
			"%s\n\nName:\n%s\n\nNotes:\n%s\n\n%s",
			titleStyle.Render(title),
			m.inputName.View(),
			m.inputNotes.View(),
			infoStyle.Render("Enter on Notes to Save"),
		))

	case viewRelationForm:
		targetName := ""
		if m.isEditing {
			// Find the other person's name for display
			otherID := m.selectedRel.ToID
			if otherID == m.selectedPerson.ID {
				otherID = m.selectedRel.FromID
			}
			targetName = getName(m.db.People, otherID)
		} else {
			targetName = m.targetPerson.Name
		}

		return docStyle.Render(fmt.Sprintf(
			"Connection with %s\n\nStrength (1-5):\n%s\n\nDescription:\n%s\n\n%s",
			titleStyle.Render(targetName),
			m.inputRelStr.View(),
			m.inputRelDesc.View(),
			infoStyle.Render("Enter on Description to Save"),
		))

	case viewConfirmDeletePerson:
		return docStyle.Render(fmt.Sprintf(
			"\n%s\n\n%s\n\n(y/n)",
			warnStyle.Render("WARNING"),
			"Do you really want to delete this person?\nAll connections to them will also be deleted.",
		))

	case viewConfirmDeleteRelation:
		return docStyle.Render(fmt.Sprintf(
			"\n%s\n\n%s\n\n(y/n)",
			warnStyle.Render("DELETE CONNECTION"),
			"Do you really want to delete this connection?",
		))
	}
	return ""
}

// --- HELPER FUNCTIONS ---

func (m *model) refreshRelationList() {
	items := []list.Item{}

	for _, r := range m.db.Relations {
		if r.FromID == m.selectedPerson.ID || r.ToID == m.selectedPerson.ID {
			otherID := r.ToID
			direction := "->"
			if r.ToID == m.selectedPerson.ID {
				otherID = r.FromID
				direction = "<-"
			}

			items = append(items, relation.RelationItem{
				Rel:       r,
				OtherName: getName(m.db.People, otherID),
				Direction: direction,
			})
		}
	}
	m.listRelations.SetItems(items)
	m.listRelations.ResetSelected()
}

func getName(people []person.Person, id string) string {
	for _, p := range people {
		if p.ID == id {
			return p.Name
		}
	}
	return "Unknown"
}

func peopleToItems(people []person.Person) []list.Item {
	items := make([]list.Item, len(people))
	for i, p := range people {
		items[i] = p
	}
	return items
}

// FIX: Automatically add IDs to old relations that don't have one
func loadData(dbPath string) db.Database {
	f, err := os.Open(dbPath)
	if err != nil {
		return db.Database{People: []person.Person{}, Relations: []relation.Relation{}, Version: config.DB_FORMAT_VERSION}
	}
	defer f.Close()
	byteValue, _ := io.ReadAll(f)
	var database db.Database
	json.Unmarshal(byteValue, &database)

	// Migration: Ensure all relations have an ID
	dirty := false
	for i := range database.Relations {
		if database.Relations[i].ID == "" {
			database.Relations[i].ID = uuid.New().String()
			dirty = true
		}
	}
	// If we fixed IDs, save back to disk immediately
	if dirty {
		saveData(database, dbPath)
	}

	return database
}

func saveData(database db.Database, dbPath string) {
	file, _ := json.MarshalIndent(database, "", " ")
	_ = os.WriteFile(dbPath, file, 0644)
}

func main() {
	// Parse Flags
	dbFlag := flag.String("db", "", "Path to the database json file")
	flag.Parse()

	// Determine path
	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = getDefaultDBPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Error creating directory %s: %v\n", dir, err)
		os.Exit(1)
	}

	fmt.Println("Saving to:", dbPath)
	p := tea.NewProgram(initialModel(dbPath), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
