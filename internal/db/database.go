package db

import (
	"github.com/N3moAhead/connect3/internal/person"
	"github.com/N3moAhead/connect3/internal/relation"
)

type Database struct {
	People    []person.Person     `json:"people"`
	Relations []relation.Relation `json:"relations"`
}
