package results

import (
	"sort"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
)

type Result struct {
	Works          []Work
	WinnerPhotoIDs []int64
	NoWinners      bool
	TotalVoters    int
}

type Work struct {
	Photo        repository.Photo
	AuthorUserID int64
	ManualVotes  int
	SelfVotes    int
	TotalVotes   int
	Winner       bool
}

func Calculate(photos []repository.Photo, votes []repository.Vote) Result {
	works := make([]Work, 0, len(photos))
	for _, photo := range photos {
		works = append(works, Work{
			Photo:        photo,
			AuthorUserID: photo.AuthorUserID,
		})
	}

	worksByPhotoID := make(map[int64]int, len(works))
	for idx := range works {
		worksByPhotoID[works[idx].Photo.ID] = idx
	}
	manualVoters := make(map[int64]struct{})
	for _, vote := range votes {
		idx, ok := worksByPhotoID[vote.PhotoID]
		if !ok {
			continue
		}
		work := &works[idx]

		switch vote.Kind {
		case repository.VoteKindManual:
			work.ManualVotes++
			work.TotalVotes++
			manualVoters[vote.VoterUserID] = struct{}{}
		case repository.VoteKindSelf:
			work.SelfVotes++
			work.TotalVotes++
		}
	}

	sort.SliceStable(works, func(i, j int) bool {
		if works[i].TotalVotes != works[j].TotalVotes {
			return works[i].TotalVotes > works[j].TotalVotes
		}
		return works[i].Photo.ID < works[j].Photo.ID
	})

	result := Result{
		Works:       works,
		NoWinners:   len(manualVoters) == 0,
		TotalVoters: len(manualVoters),
	}
	if result.NoWinners || len(works) == 0 {
		return result
	}

	maxVotes := works[0].TotalVotes
	for i := range works {
		if works[i].TotalVotes != maxVotes {
			break
		}
		works[i].Winner = true
		result.WinnerPhotoIDs = append(result.WinnerPhotoIDs, works[i].Photo.ID)
	}

	return result
}
