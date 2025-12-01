package migration

import (
	"encoding/json"
	"fmt"
	"os"
)

type Migration struct {
	FromVersion string
	ToVersion   string
	Apply       func(data map[string]any) (map[string]any, error)
}

var migrations = []Migration{
	{
		FromVersion: "0.0.1",
		ToVersion:   "1.0.0",
		Apply:       migrate_0_0_1_to_1_0_0,
	},
}

// RunMigrations will always be called on startup
func RunMigrations(dbPath string) error {
	content, err := os.ReadFile(dbPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var data map[string]any
	if err := json.Unmarshal(content, &data); err != nil {
		return err
	}

	// getting the current version
	currentVer, ok := data["version"].(string)
	if !ok {
		// Fallback if no version is provided. this case should not exist because i startet with version 0.0.1
		// but if it happens we just set version 0.0.0 and execute all migrations
		currentVer = "0.0.0"
	}

	dirty := false

	for {
		var foundMigration *Migration
		for _, m := range migrations {
			if m.FromVersion == currentVer {
				foundMigration = &m
				break
			}
		}

		if foundMigration == nil {
			break
		}

		fmt.Printf("Migrating DB from %s to %s...\n", currentVer, foundMigration.ToVersion)

		newData, err := foundMigration.Apply(data)
		if err != nil {
			return fmt.Errorf("migration failed: %v", err)
		}

		data = newData
		currentVer = foundMigration.ToVersion

		data["version"] = currentVer
		dirty = true
	}

	// Save the db if something changed...
	if dirty {
		newContent, _ := json.MarshalIndent(data, "", " ")
		return os.WriteFile(dbPath, newContent, 0644)
	}

	return nil
}

// --- Migrations ---

func migrate_0_0_1_to_1_0_0(data map[string]any) (map[string]any, error) {
	peopleRaw, ok := data["people"].([]any)
	if !ok {
		return data, nil
	}

	// Iterate through each person and add tag field
	for i, p := range peopleRaw {
		personMap, ok := p.(map[string]any)
		if !ok {
			continue
		}

		if _, hasTags := personMap["tags"]; !hasTags {
			personMap["tags"] = []string{}
		}

		peopleRaw[i] = personMap
	}

	data["people"] = peopleRaw
	return data, nil
}
