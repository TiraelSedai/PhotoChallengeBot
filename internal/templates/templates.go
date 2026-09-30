package templates

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

const (
	ChallengeAnnouncementTemplate    = "challenge_announcement.md.tmpl"
	ChallengePreviousResultsTemplate = "challenge_previous_results.md.tmpl"
	VoteStartTemplate                = "vote_start.md.tmpl"
	ResultsTemplate                  = "results.md.tmpl"
)

var announcementURLPattern = regexp.MustCompile("https?://[^\\s\\p{Z}<>()\\[\\]{}\"'`*«»“”‘’]+")

type ChallengeAnnouncementData struct {
	Num             int
	Theme           string
	Hashtag         string
	StartDate       string
	EndDate         string
	EndWeekday      string
	PrevResultsLink string
}

type VoteStartData struct {
	Theme       string
	AmountPhoto int
	VoteLink    string
	ResultsDate string
}

type ResultsData struct {
	Theme           string
	NoWinners       bool
	MultipleWinners bool
	TotalVoters     int
	Winners         []ResultLine
	Works           []ResultLine
}

type ResultLine struct {
	AuthorHandle string
	FullName     string
	Likes        int
	Winner       bool
}

type Renderer struct {
	templates *template.Template
}

func Load(dir string) (*Renderer, error) {
	pattern := filepath.Join(dir, "*.md.tmpl")

	parsed, err := template.New("").
		Funcs(template.FuncMap{
			"md":         markdownTemplateValue,
			"mdLinkText": markdownLinkTextTemplateValue,
			"mdLinkURL":  markdownLinkURLTemplateValue,
		}).
		ParseGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("load markdown templates from %s: %w", dir, err)
	}

	return &Renderer{templates: parsed}, nil
}

func (r *Renderer) Render(name string, data any) (string, error) {
	var buffer bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buffer, name, data); err != nil {
		return "", fmt.Errorf("render template %s: %w", name, err)
	}

	return buffer.String(), nil
}

func (r *Renderer) ChallengeAnnouncement(data ChallengeAnnouncementData) (string, error) {
	return r.Render(ChallengeAnnouncementTemplate, data)
}

func (r *Renderer) CustomChallengeAnnouncement(text, prevResultsLink string) (string, error) {
	if prevResultsLink == "" {
		return text, nil
	}
	for _, link := range announcementURLPattern.FindAllString(text, -1) {
		if strings.TrimRight(link, ".,;:!?_…") == prevResultsLink {
			return text, nil
		}
	}

	footer, err := r.Render(ChallengePreviousResultsTemplate, prevResultsLink)
	if err != nil {
		return "", err
	}
	footer = strings.TrimSpace(footer)
	if text == "" {
		return footer, nil
	}
	return text + "\n\n" + footer, nil
}

func (r *Renderer) VoteStart(data VoteStartData) (string, error) {
	return r.Render(VoteStartTemplate, data)
}

func (r *Renderer) Results(data ResultsData) (string, error) {
	return r.Render(ResultsTemplate, data)
}
