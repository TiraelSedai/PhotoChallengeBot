package challenge

import (
	"testing"
	"time"
)

func TestVoteTokenUsesMainChatIDAndChallengeID(t *testing.T) {
	t.Parallel()

	got := VoteToken(-1001272818469, 3)
	want := "-1001272818469_3"
	if got != want {
		t.Fatalf("VoteToken() = %q, want %q", got, want)
	}
}

func TestVoteLinkUsesBotUsernameAndStartPayload(t *testing.T) {
	t.Parallel()

	got, err := VoteLink("@photoshnaya_bot", -1001272818469, 3)
	if err != nil {
		t.Fatalf("VoteLink() error = %v", err)
	}

	want := "https://t.me/photoshnaya_bot?start=-1001272818469_3"
	if got != want {
		t.Fatalf("VoteLink() = %q, want %q", got, want)
	}
}

func TestVoteLinkRequiresBotUsername(t *testing.T) {
	t.Parallel()

	if _, err := VoteLink("", -1001, 1); err == nil {
		t.Fatal("VoteLink() error = nil, want error")
	}
}

func TestVotingEndsTextUsesMoscowTime(t *testing.T) {
	t.Parallel()

	value := time.Date(2026, 5, 20, 15, 0, 0, 0, time.UTC)

	got := VotingEndsText(value, moscowLocation)
	want := "20 мая в 18:00 МСК"
	if got != want {
		t.Fatalf("VotingEndsText() = %q, want %q", got, want)
	}
}

func TestVotingEndsTextUsesProvidedLocation(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+5", 5*60*60)
	value := time.Date(2026, 5, 20, 15, 0, 0, 0, time.UTC)

	got := VotingEndsText(value, location)
	want := "20 мая в 20:00 UTC+5"
	if got != want {
		t.Fatalf("VotingEndsText() = %q, want %q", got, want)
	}
}
