package relation

import (
	"fmt"
)

type Relation struct {
	ID          string `json:"id"`
	FromID      string `json:"from_id"`
	ToID        string `json:"to_id"`
	Strength    int    `json:"strength"` // 1-5
	Description string `json:"description"`
}

// Wrapper for Relation to be used in bubbles/list
type RelationItem struct {
	Rel       Relation
	OtherName string
	Direction string // "->" or "<-"
}

func (r RelationItem) Title() string {
	icon := "⚪"
	switch r.Rel.Strength {
	case 2:
		icon = "🔵"
	case 3:
		icon = "🟢"
	case 4:
		icon = "🟡"
	case 5:
		icon = "🔴"
	}
	return fmt.Sprintf("%s %s %s (%d/5)", icon, r.Direction, r.OtherName, r.Rel.Strength)
}
func (r RelationItem) Description() string { return r.Rel.Description }
func (r RelationItem) FilterValue() string { return r.OtherName + " " + r.Rel.Description }
