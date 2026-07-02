package results

import (
	"reflect"
	"testing"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
)

func TestCalculateSortsByTotalVotesAndMarksTiedWinners(t *testing.T) {
	photos := []repository.Photo{
		{ID: 30, ChallengeID: 1, AuthorUserID: 300},
		{ID: 10, ChallengeID: 1, AuthorUserID: 100},
		{ID: 20, ChallengeID: 1, AuthorUserID: 200},
	}
	votes := []repository.Vote{
		{ChallengeID: 1, VoterUserID: 1, PhotoID: 30, Kind: repository.VoteKindManual},
		{ChallengeID: 1, VoterUserID: 2, PhotoID: 30, Kind: repository.VoteKindSelf},
		{ChallengeID: 1, VoterUserID: 3, PhotoID: 20, Kind: repository.VoteKindManual},
		{ChallengeID: 1, VoterUserID: 4, PhotoID: 20, Kind: repository.VoteKindManual},
		{ChallengeID: 1, VoterUserID: 3, PhotoID: 10, Kind: repository.VoteKindManual},
	}

	result := Calculate(photos, votes)

	if result.NoWinners {
		t.Fatalf("NoWinners = true, want false")
	}
	if !reflect.DeepEqual(result.WinnerPhotoIDs, []int64{20, 30}) {
		t.Fatalf("WinnerPhotoIDs = %#v, want [20 30]", result.WinnerPhotoIDs)
	}
	if result.TotalVoters != 3 {
		t.Fatalf("TotalVoters = %d, want 3 (unique manual voters, self votes excluded)", result.TotalVoters)
	}

	gotRows := summarize(result.Works)
	wantRows := []workSummary{
		{photoID: 20, authorID: 200, manualVotes: 2, selfVotes: 0, totalVotes: 2, winner: true},
		{photoID: 30, authorID: 300, manualVotes: 1, selfVotes: 1, totalVotes: 2, winner: true},
		{photoID: 10, authorID: 100, manualVotes: 1, selfVotes: 0, totalVotes: 1, winner: false},
	}
	if !reflect.DeepEqual(gotRows, wantRows) {
		t.Fatalf("Works = %#v, want %#v", gotRows, wantRows)
	}
}

func TestCalculateHasNoWinnersWithoutManualVotes(t *testing.T) {
	photos := []repository.Photo{
		{ID: 10, ChallengeID: 1, AuthorUserID: 100},
		{ID: 20, ChallengeID: 1, AuthorUserID: 200},
	}
	votes := []repository.Vote{
		{ChallengeID: 1, VoterUserID: 1, PhotoID: 10, Kind: repository.VoteKindSelf},
		{ChallengeID: 1, VoterUserID: 2, PhotoID: 20, Kind: repository.VoteKindSelf},
		{ChallengeID: 1, VoterUserID: 3, PhotoID: 20, Kind: repository.VoteKindSelf},
	}

	result := Calculate(photos, votes)

	if !result.NoWinners {
		t.Fatalf("NoWinners = false, want true")
	}
	if len(result.WinnerPhotoIDs) != 0 {
		t.Fatalf("WinnerPhotoIDs = %#v, want empty", result.WinnerPhotoIDs)
	}
	if result.TotalVoters != 0 {
		t.Fatalf("TotalVoters = %d, want 0", result.TotalVoters)
	}

	gotRows := summarize(result.Works)
	wantRows := []workSummary{
		{photoID: 20, authorID: 200, manualVotes: 0, selfVotes: 2, totalVotes: 2, winner: false},
		{photoID: 10, authorID: 100, manualVotes: 0, selfVotes: 1, totalVotes: 1, winner: false},
	}
	if !reflect.DeepEqual(gotRows, wantRows) {
		t.Fatalf("Works = %#v, want %#v", gotRows, wantRows)
	}
}

func TestCalculateIgnoresVotesForAbsentPhotosAndUsesPhotoIDTieBreak(t *testing.T) {
	photos := []repository.Photo{
		{ID: 30, ChallengeID: 1, AuthorUserID: 300},
		{ID: 10, ChallengeID: 1, AuthorUserID: 100},
	}
	votes := []repository.Vote{
		{ChallengeID: 1, VoterUserID: 1, PhotoID: 30, Kind: repository.VoteKindManual},
		{ChallengeID: 1, VoterUserID: 2, PhotoID: 10, Kind: repository.VoteKindManual},
		{ChallengeID: 1, VoterUserID: 3, PhotoID: 999, Kind: repository.VoteKindManual},
	}

	result := Calculate(photos, votes)

	if result.TotalVoters != 2 {
		t.Fatalf("TotalVoters = %d, want 2 (vote for absent photo ignored)", result.TotalVoters)
	}
	gotRows := summarize(result.Works)
	wantRows := []workSummary{
		{photoID: 10, authorID: 100, manualVotes: 1, selfVotes: 0, totalVotes: 1, winner: true},
		{photoID: 30, authorID: 300, manualVotes: 1, selfVotes: 0, totalVotes: 1, winner: true},
	}
	if !reflect.DeepEqual(gotRows, wantRows) {
		t.Fatalf("Works = %#v, want %#v", gotRows, wantRows)
	}
}

type workSummary struct {
	photoID     int64
	authorID    int64
	manualVotes int
	selfVotes   int
	totalVotes  int
	winner      bool
}

func summarize(works []Work) []workSummary {
	summary := make([]workSummary, 0, len(works))
	for _, work := range works {
		summary = append(summary, workSummary{
			photoID:     work.Photo.ID,
			authorID:    work.AuthorUserID,
			manualVotes: work.ManualVotes,
			selfVotes:   work.SelfVotes,
			totalVotes:  work.TotalVotes,
			winner:      work.Winner,
		})
	}
	return summary
}
