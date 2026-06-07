package templates

import (
	"bytes"
	"fmt"
	"path/filepath"
	"text/template"
)

const (
	ChallengeAnnouncementTemplate = "challenge_announcement.md.tmpl"
	VoteStartTemplate             = "vote_start.md.tmpl"
	ResultsTemplate               = "results.md.tmpl"
)

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

func (r *Renderer) VoteStart(data VoteStartData) (string, error) {
	return r.Render(VoteStartTemplate, data)
}

func (r *Renderer) Results(data ResultsData) (string, error) {
	return r.Render(ResultsTemplate, data)
}
