package topic

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/hashtag"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/publish"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
)

const (
	topicPollQuestion   = "Выбираем тему нового челленджа. Голосуем до пятницы, можно за несколько✨"
	maxPollOptions      = 12
	maxPollOptionLength = 100
)

func (r *Reporter) publishPoll(ctx context.Context, challenge repository.Challenge) error {
	if challenge.State != repository.ChallengeStateFinished || challenge.TopicPollSentAt != nil {
		return nil
	}
	_, err := publish.Attempt(ctx,
		publish.Config{PersistTimeout: r.persistFor},
		publish.Stage{Claim: r.challenges.ClaimTopicPoll, Release: r.challenges.ReleaseTopicPollClaim},
		challenge.ID, r.now(),
		func(ctx context.Context, l *publish.Lease) error {
			suggestions, err := r.suggestions.ListByChallenge(ctx, challenge.ID)
			if err != nil {
				_ = l.Release(ctx)
				return err
			}
			options := r.pollOptions(suggestions)
			if len(options) > 0 {
				sendCtx, cancel := context.WithTimeout(ctx, r.sendFor)
				_, err = r.publisher.SendPoll(sendCtx, challenge.MainChatID, topicPollQuestion, options)
				cancel()
				if err != nil {
					_ = l.Release(ctx)
					return fmt.Errorf("send topic poll for challenge %d: %w", challenge.ID, err)
				}
			}
			return l.Commit(ctx, fmt.Sprintf("mark topic poll sent for challenge %d", challenge.ID),
				func(pctx context.Context) (bool, error) {
					return r.challenges.MarkTopicPollSent(pctx, challenge.ID, l.ClaimedAt, r.now())
				})
		})
	return err
}

func (r *Reporter) pollOptions(suggestions []repository.TopicSuggestion) []string {
	// Work on a copy: the admin report keeps every original suggestion.
	candidates := make([]repository.TopicSuggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		text := hashtag.Remove(suggestion.Text, themeSuggestionHashtag)
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > maxPollOptionLength {
			text = string(runes[:maxPollOptionLength-len(truncatedSuffix)]) + truncatedSuffix
		}
		suggestion.Text = text
		candidates = append(candidates, suggestion)
	}

	for len(candidates) > maxPollOptions {
		counts := make(map[int64]int)
		for _, suggestion := range candidates {
			counts[suggestion.AuthorUserID]++
		}
		var duplicateAuthors []int64
		for _, suggestion := range candidates {
			if counts[suggestion.AuthorUserID] > 1 && !slices.Contains(duplicateAuthors, suggestion.AuthorUserID) {
				duplicateAuthors = append(duplicateAuthors, suggestion.AuthorUserID)
			}
		}
		var remove int
		if len(duplicateAuthors) == 0 {
			remove = r.randomIndex(len(candidates))
		} else {
			author := duplicateAuthors[r.randomIndex(len(duplicateAuthors))]
			var indices []int
			for idx, suggestion := range candidates {
				if suggestion.AuthorUserID == author {
					indices = append(indices, idx)
				}
			}
			remove = indices[r.randomIndex(len(indices))]
		}
		candidates = slices.Delete(candidates, remove, remove+1)
	}

	options := make([]string, 0, len(candidates))
	for _, suggestion := range candidates {
		options = append(options, suggestion.Text)
	}
	return options
}
