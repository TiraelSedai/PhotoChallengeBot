package history

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const dateLayout = "2.1.06"

const (
	colNum     = 0
	colPlayed  = 1
	colStart   = 2
	colEnd     = 3
	colTheme   = 4
	colHashtag = 5
	colWinner  = 10
)

type Record struct {
	Num     int
	Start   time.Time
	End     time.Time
	Theme   string
	Hashtag string
	Winners []string
}

// Parse reads the historical challenges export. Rows with an empty challenge
// number continue the previous challenge with an extra winner; FALSE rows are
// unplayed theme ideas and are skipped.
func Parse(r io.Reader, loc *time.Location) ([]Record, []string, error) {
	if loc == nil {
		return nil, nil, fmt.Errorf("parse history: location is required")
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("parse history: read csv: %w", err)
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = sniffDelimiter(data)
	reader.FieldsPerRecord = -1

	var records []Record
	var warnings []string
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse history: read csv: %w", err)
		}

		numField := field(row, colNum)
		played := strings.EqualFold(field(row, colPlayed), "TRUE")
		switch {
		case numField != "":
			num, err := strconv.Atoi(numField)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skipped row with non-numeric challenge number %q", numField))
				continue
			}
			if !played {
				continue
			}
			record, rowWarnings, err := parseChallengeRow(row, num, loc)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, rowWarnings...)
			if last := len(records) - 1; last >= 0 {
				if num <= records[last].Num {
					return nil, nil, fmt.Errorf("parse history: challenge %d after %d: numbers must increase", num, records[last].Num)
				}
				if num != records[last].Num+1 {
					warnings = append(warnings, fmt.Sprintf("challenge numbers jump from %d to %d", records[last].Num, num))
				}
			}
			records = append(records, record)
		case played:
			if len(records) == 0 {
				return nil, nil, fmt.Errorf("parse history: extra winner row before any challenge")
			}
			winner := normalizeWinner(field(row, colWinner))
			if winner == "" {
				warnings = append(warnings, fmt.Sprintf("challenge %d: extra winner row without username", records[len(records)-1].Num))
				continue
			}
			addWinner(&records[len(records)-1], winner, &warnings)
		}
	}

	if len(records) == 0 {
		return nil, nil, fmt.Errorf("parse history: no challenges found")
	}
	return records, warnings, nil
}

func parseChallengeRow(row []string, num int, loc *time.Location) (Record, []string, error) {
	start, err := time.ParseInLocation(dateLayout, field(row, colStart), loc)
	if err != nil {
		return Record{}, nil, fmt.Errorf("parse history: challenge %d: start date %q: %w", num, field(row, colStart), err)
	}
	end, err := time.ParseInLocation(dateLayout, field(row, colEnd), loc)
	if err != nil {
		return Record{}, nil, fmt.Errorf("parse history: challenge %d: end date %q: %w", num, field(row, colEnd), err)
	}
	theme := field(row, colTheme)
	if theme == "" {
		return Record{}, nil, fmt.Errorf("parse history: challenge %d: theme is empty", num)
	}

	var warnings []string
	if end.Before(start) {
		warnings = append(warnings, fmt.Sprintf("challenge %d: end %s is before start %s, imported as is", num, end.Format("02.01.2006"), start.Format("02.01.2006")))
	}
	hashtag := normalizeHashtag(field(row, colHashtag))
	if hashtag == "" {
		warnings = append(warnings, fmt.Sprintf("challenge %d: hashtag is empty", num))
	}

	record := Record{Num: num, Start: start, End: end, Theme: theme, Hashtag: hashtag}
	if winner := normalizeWinner(field(row, colWinner)); winner != "" {
		record.Winners = append(record.Winners, winner)
	} else {
		warnings = append(warnings, fmt.Sprintf("challenge %d: winner username is empty", num))
	}
	return record, warnings, nil
}

func addWinner(record *Record, winner string, warnings *[]string) {
	for _, existing := range record.Winners {
		if strings.EqualFold(existing, winner) {
			*warnings = append(*warnings, fmt.Sprintf("challenge %d: duplicate winner %q", record.Num, winner))
			return
		}
	}
	record.Winners = append(record.Winners, winner)
}

func normalizeWinner(username string) string {
	return strings.TrimPrefix(strings.TrimSpace(username), "@")
}

func normalizeHashtag(raw string) string {
	tag := strings.TrimSpace(raw)
	if tag == "" {
		return ""
	}
	if !strings.HasPrefix(tag, "#") {
		tag = "#" + tag
	}
	return tag
}

// Spreadsheet exports come comma- or semicolon-delimited depending on locale.
func sniffDelimiter(data []byte) rune {
	line, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.Count(line, []byte(";")) > bytes.Count(line, []byte(",")) {
		return ';'
	}
	return ','
}

func field(row []string, idx int) string {
	if idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}
