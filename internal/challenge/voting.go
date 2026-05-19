package challenge

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

var moscowLocation = func() *time.Location {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return location
}()

const votingDuration = 48 * time.Hour

func VoteToken(mainChatID, challengeID int64) string {
	// This is an intentionally forgeable routing token, not an auth boundary.
	return fmt.Sprintf("%d_%d", mainChatID, challengeID)
}

func VoteLink(botUsername string, mainChatID, challengeID int64) (string, error) {
	username := strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	if username == "" {
		return "", fmt.Errorf("bot username is required")
	}

	link := url.URL{
		Scheme:   "https",
		Host:     "t.me",
		Path:     "/" + username,
		RawQuery: "start=" + url.QueryEscape(VoteToken(mainChatID, challengeID)),
	}
	return link.String(), nil
}

func VotingEndsAt(startedAt time.Time) time.Time {
	return startedAt.Add(votingDuration)
}

func VotingEndsText(value time.Time, location *time.Location) string {
	if location == nil {
		location = moscowLocation
	}
	local := value.In(location)
	zone, _ := local.Zone()
	if zone == "" {
		zone = location.String()
	}
	if zone == "MSK" {
		zone = "МСК"
	}
	return fmt.Sprintf("%d %s в %02d:%02d %s", local.Day(), russianMonth(local.Month()), local.Hour(), local.Minute(), zone)
}

func russianMonth(month time.Month) string {
	months := [...]string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	if month < time.January || month > time.December {
		return ""
	}
	return months[int(month)-1]
}
