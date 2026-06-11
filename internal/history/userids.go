package history

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseUserIDs reads the "username,telegram-id" mapping used to backfill
// winner user IDs. Keys are lowercased; rows with a non-numeric id (e.g.
// "n/a") are skipped with a warning.
func ParseUserIDs(r io.Reader) (map[string]int64, []string, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	ids := make(map[string]int64)
	var warnings []string
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse user ids: read csv: %w", err)
		}
		username := normalizeWinner(field(row, 0))
		if username == "" {
			continue
		}
		id, err := strconv.ParseInt(field(row, 1), 10, 64)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("user id for %q is not numeric (%q), skipped", username, field(row, 1)))
			continue
		}
		key := strings.ToLower(username)
		if existing, ok := ids[key]; ok && existing != id {
			warnings = append(warnings, fmt.Sprintf("conflicting user ids for %q, keeping %d", username, existing))
			continue
		}
		ids[key] = id
	}

	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("parse user ids: no mappings found")
	}
	return ids, warnings, nil
}
