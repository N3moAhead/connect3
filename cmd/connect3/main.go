package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/N3moAhead/connect3/internal/config"
	"github.com/N3moAhead/connect3/internal/db"
	"github.com/N3moAhead/connect3/internal/migration"
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
	tagStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).MarginRight(1)
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
	viewTagSelect
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

	// Tag Selection
	listTags list.Model      // list of available tags
	inputTag textinput.Model // Dedicated input for tags
	tempTags []string        // list of tags which we are editing
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
	lr.SetFilteringEnabled(false)
	lr.SetShowHelp(false)
	lr.SetShowStatusBar(false)
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

	// 4. Init Tag List & Input
	initialTags := getAllUniqueTags(database.People)
	lt := list.New(initialTags, list.NewDefaultDelegate(), 0, 0)
	lt.SetShowTitle(false)
	lt.SetShowStatusBar(false)
	lt.SetFilteringEnabled(false) // Wir machen unser eigenes Filtering
	lt.SetShowHelp(false)
	lt.DisableQuitKeybindings()

	tiTag := textinput.New()
	tiTag.Placeholder = "Type to search or create new tag..."
	tiTag.CharLimit = 30

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
		listTags:      lt,
		inputTag:      tiTag,
		tempTags:      []string{},
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
		h, v := docStyle.GetFrameSize()
		m.listPeople.SetSize(msg.Width-h, msg.Height-v)

		relHeight := max(msg.Height-v-12, 5)
		m.listRelations.SetSize(msg.Width-h, relHeight)

		tagListH := msg.Height - v - 6
		if tagListH < 1 {
			tagListH = 1
		}
		m.listTags.SetSize(msg.Width-h, tagListH)
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
				m.tempTags = []string{}
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
					m.refreshRelationList()
				}
				return m, nil
			}
		}
		m.listPeople, cmd = m.listPeople.Update(msg)
		return m, cmd

	// ---------------------------------------------------------
	// 2. DETAIL VIEW
	// ---------------------------------------------------------
	case viewDetail:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewListPeople
				m.selectedPerson = nil
				return m, nil
			case "E": // Edit Person
				m.tempTags = m.selectedPerson.Tags
				m.state = viewPersonForm
				m.isEditing = true
				m.inputName.SetValue(m.selectedPerson.Name)
				m.inputNotes.SetValue(m.selectedPerson.Notes)
				m.inputName.Focus()
				m.inputNotes.Blur()
				return m, nil

			// --- Ctrl+g für Tags ---
			case "ctrl+g":
				m.tempTags = m.selectedPerson.Tags
				m.isEditing = true
				m.inputName.SetValue(m.selectedPerson.Name)
				m.inputNotes.SetValue(m.selectedPerson.Notes)

				// Reset Tag View
				m.inputTag.SetValue("")
				m.inputTag.Focus()
				m.updateTagListFilter() // Initial list population
				m.state = viewTagSelect
				return m, nil

			case "D": // Delete Person
				m.state = viewConfirmDeletePerson
				return m, nil

			// Relation Logic
			case "n":
				m.state = viewRelationTarget
				m.listPeople.Title = "Select person to connect with " + m.selectedPerson.Name
				m.listPeople.ResetSelected()
				return m, nil
			case "e":
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
				if len(m.listRelations.Items()) > 0 {
					if i, ok := m.listRelations.SelectedItem().(relation.RelationItem); ok {
						m.selectedRel = &i.Rel
						m.state = viewConfirmDeleteRelation
					}
				}
				return m, nil
			}
		}
		m.listRelations, cmd = m.listRelations.Update(msg)
		return m, cmd

	// ---------------------------------------------------------
	// 3. PERSON FORM
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
				return m, nil

			// --- Ctrl+g für Tags ---
			case "ctrl+g":
				m.inputTag.SetValue("")
				m.inputTag.Focus()
				m.updateTagListFilter()
				m.state = viewTagSelect

				m.inputName.Blur()
				m.inputNotes.Blur()
				return m, nil

			case "enter":
				if m.inputName.Focused() {
					m.inputName.Blur()
					m.inputNotes.Focus()
					return m, nil
				}
				// Save Logic
				if m.isEditing {
					for i, p := range m.db.People {
						if p.ID == m.selectedPerson.ID {
							m.db.People[i].Name = m.inputName.Value()
							m.db.People[i].Notes = m.inputNotes.Value()
							m.db.People[i].Tags = m.tempTags
							m.selectedPerson = &m.db.People[i]
							break
						}
					}
				} else {
					newP := person.Person{
						ID:    uuid.New().String(),
						Name:  m.inputName.Value(),
						Notes: m.inputNotes.Value(),
						Tags:  m.tempTags,
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
	// 4. TAG SELECTION VIEW
	// ---------------------------------------------------------
	case viewTagSelect:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.state = viewPersonForm
				m.inputName.Focus()
				return m, nil

			case "down", "up":
				m.listTags, cmd = m.listTags.Update(msg)
				return m, cmd

			case "enter":
				selectedTag := ""
				if len(m.listTags.Items()) > 0 && m.listTags.SelectedItem() != nil {
					if i, ok := m.listTags.SelectedItem().(item); ok {
						selectedTag = string(i)
					}
				} else {
					selectedTag = strings.TrimSpace(m.inputTag.Value())
				}

				if selectedTag == "" && m.inputTag.Value() != "" {
					selectedTag = strings.TrimSpace(m.inputTag.Value())
				}

				if selectedTag != "" {
					exists := false
					for _, t := range m.tempTags {
						if t == selectedTag {
							exists = true
						}
					}
					if !exists {
						m.tempTags = append(m.tempTags, selectedTag)
					}
				}

				m.state = viewPersonForm
				m.inputName.Focus()
				return m, nil
			}

			var cmdInput tea.Cmd
			m.inputTag, cmdInput = m.inputTag.Update(msg)
			m.updateTagListFilter()
			return m, cmdInput
		}

	// ---------------------------------------------------------
	// 5. CONFIRM DELETE
	// ---------------------------------------------------------
	case viewConfirmDeletePerson:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "y" || msg.String() == "Y" {
				newRels := []relation.Relation{}
				for _, r := range m.db.Relations {
					if r.FromID != m.selectedPerson.ID && r.ToID != m.selectedPerson.ID {
						newRels = append(newRels, r)
					}
				}
				m.db.Relations = newRels
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
	// 6. RELATION STUFF
	// ---------------------------------------------------------
	case viewRelationTarget:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" {
				m.state = viewDetail
				m.listPeople.Title = "People"
				return m, nil
			}
			if msg.String() == "enter" {
				if i, ok := m.listPeople.SelectedItem().(person.Person); ok {
					if i.ID == m.selectedPerson.ID {
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
				strVal, _ := strconv.Atoi(m.inputRelStr.Value())
				if strVal < 1 {
					strVal = 1
				}
				if strVal > 5 {
					strVal = 5
				}
				if m.isEditing {
					for i, r := range m.db.Relations {
						if r.ID == m.selectedRel.ID {
							m.db.Relations[i].Strength = strVal
							m.db.Relations[i].Description = m.inputRelDesc.Value()
							break
						}
					}
				} else {
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
				m.listPeople.Title = "People"
				return m, nil
			}
		}
		var cmdStr, cmdDesc tea.Cmd
		m.inputRelStr, cmdStr = m.inputRelStr.Update(msg)
		m.inputRelDesc, cmdDesc = m.inputRelDesc.Update(msg)
		return m, tea.Batch(cmdStr, cmdDesc)

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
		tagBlock := ""
		if len(m.selectedPerson.Tags) > 0 {
			for _, t := range m.selectedPerson.Tags {
				tagBlock += tagStyle.Render("#" + t)
			}
			tagBlock += "\n\n"
		}
		s := titleStyle.Render(m.selectedPerson.Name) + "\n"
		s += infoStyle.Render(m.selectedPerson.Notes) + "\n\n"
		s += tagBlock
		help := infoStyle.Render("E: Edit Person | D: Delete Person | Ctrl+g: Tags | n: New Rel | e: Edit Rel | d: Del Rel | ESC: Back")
		s += help + "\n\n"
		s += lipgloss.NewStyle().Underline(true).Render("Connections:") + "\n"
		s += m.listRelations.View()
		return docStyle.Render(s)

	case viewPersonForm:
		title := "Create New Person"
		if m.isEditing {
			title = "Edit Person"
		}
		tagsStr := ""
		for _, t := range m.tempTags {
			tagsStr += tagStyle.Render("#" + t)
		}
		if tagsStr == "" {
			tagsStr = infoStyle.Render("(No tags - Press Ctrl+g to add)")
		}
		return docStyle.Render(fmt.Sprintf(
			"%s\n\nName:\n%s\n\nNotes:\n%s\n\nTags:\n%s\n\n%s",
			titleStyle.Render(title),
			m.inputName.View(),
			m.inputNotes.View(),
			tagsStr,
			infoStyle.Render("Enter on Notes to Save | Ctrl+g: Manage Tags"),
		))

	case viewTagSelect:
		listView := m.listTags.View()
		// Wir entfernen die "empty check" hier, weil die Liste jetzt korrekt rendert,
		// auch wenn sie leer ist (dann halt leer).
		if len(m.listTags.Items()) == 0 {
			listView = infoStyle.Render("(No existing tags found - Type to create new)")
		}

		return docStyle.Render(fmt.Sprintf(
			"%s\n\n%s\n\n%s\n%s",
			titleStyle.Render("Manage Tags"),
			m.inputTag.View(),
			infoStyle.Render("Existing Tags:"),
			listView,
		))

	case viewRelationForm:
		// ... (wie gehabt)
		targetName := ""
		if m.isEditing {
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
		return docStyle.Render(fmt.Sprintf("\n%s\n\n%s\n\n(y/n)", warnStyle.Render("WARNING"), "Do you really want to delete this person?"))
	case viewConfirmDeleteRelation:
		return docStyle.Render(fmt.Sprintf("\n%s\n\n%s\n\n(y/n)", warnStyle.Render("DELETE CONNECTION"), "Do you really want to delete this connection?"))
	}
	return ""
}

// --- HELPER FUNCTIONS ---

type item string

func (i item) FilterValue() string { return string(i) }

// FIX: Title und Description implementieren, damit der DefaultDelegate was anzeigen kann
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

func (m *model) updateTagListFilter() {
	term := strings.ToLower(strings.TrimSpace(m.inputTag.Value()))

	// 1. Tags aus DB holen
	dbTags := getAllUniqueTags(m.db.People)

	// 2. Map erstellen
	tagMap := make(map[string]bool)
	for _, loopItem := range dbTags {
		tagMap[string(loopItem.(item))] = true
	}

	// 3. Temp-Tags hinzufügen
	for _, t := range m.tempTags {
		tagMap[t] = true
	}

	// 4. Filtern
	items := []list.Item{}
	for t := range tagMap {
		if term == "" || strings.Contains(strings.ToLower(t), term) {
			items = append(items, item(t))
		}
	}

	// Sortieren
	sort.Slice(items, func(i, j int) bool {
		return string(items[i].(item)) < string(items[j].(item))
	})

	m.listTags.SetItems(items)
	m.listTags.ResetSelected()
}

func getAllUniqueTags(people []person.Person) []list.Item {
	tagMap := make(map[string]bool)
	for _, p := range people {
		for _, t := range p.Tags {
			tagMap[t] = true
		}
	}
	items := []list.Item{}
	for t := range tagMap {
		items = append(items, item(t))
	}
	return items
}

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

func loadData(dbPath string) db.Database {
	f, err := os.Open(dbPath)
	if err != nil {
		return db.Database{People: []person.Person{}, Relations: []relation.Relation{}, Version: config.DB_FORMAT_VERSION}
	}
	defer f.Close()
	byteValue, _ := io.ReadAll(f)
	var database db.Database
	json.Unmarshal(byteValue, &database)

	dirty := false
	for i := range database.Relations {
		if database.Relations[i].ID == "" {
			database.Relations[i].ID = uuid.New().String()
			dirty = true
		}
	}
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
	dbFlag := flag.String("db", "", "Path to the database json file")
	flag.Parse()
	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = getDefaultDBPath()
	}
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Error creating directory %s: %v\n", dir, err)
		os.Exit(1)
	}
	if err := migration.RunMigrations(dbPath); err != nil {
		fmt.Printf("Error running migrations: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Saving to:", dbPath)
	p := tea.NewProgram(initialModel(dbPath), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
