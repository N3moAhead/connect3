package person

type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Notes string `json:"notes"`
}

// Implement list.Item interface
func (p Person) Title() string       { return p.Name }
func (p Person) Description() string { return p.Notes }
func (p Person) FilterValue() string { return p.Name }
