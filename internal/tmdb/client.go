package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
)

var ErrDisabled = errors.New("tmdb scraping is disabled")

type Client struct {
	baseURL    string
	imageURL   string
	token      string
	apiKey     string
	language   string
	languages  []string
	region     string
	people     bool
	httpClient *http.Client
}

type Episode struct {
	ShowID       int
	EpisodeID    int
	ShowName     string
	Title        string
	Overview     string
	AirDate      string
	VoteAverage  float64
	StillPath    string
	EpisodeGroup string
	Actors       []Actor
	Crew         []CrewMember
}

type Actor struct {
	Name        string
	Role        string
	Order       int
	ProfilePath string
}

type CrewMember struct {
	Name        string
	Job         string
	ProfilePath string
}

type searchTVResponse struct {
	Results []tvSearchResult `json:"results"`
}

type tvSearchResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	FirstAirDate string `json:"first_air_date"`
	Overview     string `json:"overview"`
}

type episodeResponse struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Overview    string  `json:"overview"`
	AirDate     string  `json:"air_date"`
	VoteAverage float64 `json:"vote_average"`
	StillPath   string  `json:"still_path"`
}

type episodeCreditsResponse struct {
	Cast       []creditPerson `json:"cast"`
	Crew       []creditPerson `json:"crew"`
	GuestStars []creditPerson `json:"guest_stars"`
}

type creditPerson struct {
	Name        string `json:"name"`
	Character   string `json:"character"`
	Job         string `json:"job"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

func NewClient(cfg config.ScrapingConfig) (*Client, error) {
	if !cfg.EnableTMDB {
		return nil, ErrDisabled
	}
	if strings.TrimSpace(cfg.TMDBToken) == "" && strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, ErrDisabled
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.TMDBBaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.themoviedb.org/3"
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(cfg.Proxy) != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("parse tmdb proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &Client{
		baseURL:  baseURL,
		imageURL: "https://image.tmdb.org/t/p/original",
		token:    strings.TrimSpace(cfg.TMDBToken),
		apiKey:   strings.TrimSpace(cfg.TMDBAPIKey),
		language: defaultString(cfg.Language, "zh-CN"),
		languages: languageOrder(
			defaultString(cfg.Language, "zh-CN"),
			cfg.FallbackLanguages,
		),
		region: defaultString(cfg.Region, "CN"),
		people: cfg.EnablePeople,
		httpClient: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
		},
	}, nil
}

func (c *Client) ImageURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return c.imageURL + path
}

func (c *Client) FindEpisode(ctx context.Context, showQuery string, season int, episode int) (Episode, error) {
	showQuery = strings.TrimSpace(showQuery)
	if showQuery == "" {
		return Episode{}, errors.New("tmdb show query is empty")
	}

	show, err := c.searchTV(ctx, showQuery)
	if err != nil {
		return Episode{}, err
	}
	if show.ID == 0 {
		return Episode{}, fmt.Errorf("tmdb tv show not found: %s", showQuery)
	}

	episodeDetail, err := c.getEpisodeWithFallback(ctx, show.ID, season, episode)
	if err != nil {
		return Episode{}, err
	}
	var credits episodeCredits
	if c.people {
		credits, err = c.getEpisodeCredits(ctx, show.ID, season, episode)
		if err != nil {
			return Episode{}, err
		}
	}

	return Episode{
		ShowID:      show.ID,
		EpisodeID:   episodeDetail.ID,
		ShowName:    firstNonEmpty(show.Name, show.OriginalName),
		Title:       episodeDetail.Name,
		Overview:    episodeDetail.Overview,
		AirDate:     episodeDetail.AirDate,
		VoteAverage: episodeDetail.VoteAverage,
		StillPath:   episodeDetail.StillPath,
		Actors:      credits.Actors,
		Crew:        credits.Crew,
	}, nil
}

func (c *Client) searchTV(ctx context.Context, query string) (tvSearchResult, error) {
	var parsed searchTVResponse
	if err := c.get(ctx, c.language, "/search/tv", url.Values{"query": {query}}, &parsed); err != nil {
		return tvSearchResult{}, err
	}
	if len(parsed.Results) == 0 {
		return tvSearchResult{}, nil
	}
	return parsed.Results[0], nil
}

func (c *Client) getEpisodeWithFallback(ctx context.Context, showID int, season int, episode int) (episodeResponse, error) {
	var merged episodeResponse
	var firstErr error
	for _, language := range c.languages {
		detail, err := c.getEpisode(ctx, language, showID, season, episode)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		mergeEpisode(&merged, detail)
		if episodeComplete(merged) {
			return merged, nil
		}
	}
	if merged.ID != 0 {
		return merged, nil
	}
	if firstErr != nil {
		return episodeResponse{}, firstErr
	}
	return episodeResponse{}, errors.New("tmdb episode not found")
}

func (c *Client) getEpisode(ctx context.Context, language string, showID int, season int, episode int) (episodeResponse, error) {
	var parsed episodeResponse
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, season, episode)
	if err := c.get(ctx, language, path, nil, &parsed); err != nil {
		return episodeResponse{}, err
	}
	return parsed, nil
}

type episodeCredits struct {
	Actors []Actor
	Crew   []CrewMember
}

func (c *Client) getEpisodeCredits(ctx context.Context, showID int, season int, episode int) (episodeCredits, error) {
	var parsed episodeCreditsResponse
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d/credits", showID, season, episode)
	if err := c.get(ctx, c.language, path, nil, &parsed); err != nil {
		return episodeCredits{}, err
	}

	actors := make([]Actor, 0, len(parsed.Cast)+len(parsed.GuestStars))
	seen := map[string]struct{}{}
	appendCredit := func(person creditPerson) {
		name := strings.TrimSpace(person.Name)
		if name == "" {
			return
		}
		key := strings.ToLower(name + "\x00" + strings.TrimSpace(person.Character))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		actors = append(actors, Actor{Name: name, Role: strings.TrimSpace(person.Character), Order: person.Order, ProfilePath: person.ProfilePath})
	}
	for _, person := range parsed.Cast {
		appendCredit(person)
	}
	for _, person := range parsed.GuestStars {
		appendCredit(person)
	}

	crew := make([]CrewMember, 0, len(parsed.Crew))
	seenCrew := map[string]struct{}{}
	for _, person := range parsed.Crew {
		name := strings.TrimSpace(person.Name)
		job := strings.TrimSpace(person.Job)
		if name == "" || job == "" {
			continue
		}
		key := strings.ToLower(name + "\x00" + job)
		if _, ok := seenCrew[key]; ok {
			continue
		}
		seenCrew[key] = struct{}{}
		crew = append(crew, CrewMember{Name: name, Job: job, ProfilePath: person.ProfilePath})
	}
	return episodeCredits{Actors: actors, Crew: crew}, nil
}

func (c *Client) get(ctx context.Context, language string, path string, query url.Values, target any) error {
	if query == nil {
		query = url.Values{}
	}
	query.Set("language", language)
	if c.region != "" {
		query.Set("region", c.region)
	}
	if c.token == "" && c.apiKey != "" {
		query.Set("api_key", c.apiKey)
	}

	requestURL := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tmdb request failed: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func languageOrder(primary string, fallback []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	appendLanguage := func(language string) {
		language = strings.TrimSpace(language)
		if language == "" {
			return
		}
		key := strings.ToLower(language)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, language)
	}
	appendLanguage(primary)
	for _, language := range fallback {
		appendLanguage(language)
	}
	return result
}

func mergeEpisode(target *episodeResponse, source episodeResponse) {
	if target.ID == 0 {
		target.ID = source.ID
	}
	if target.Name == "" {
		target.Name = strings.TrimSpace(source.Name)
	}
	if target.Overview == "" {
		target.Overview = strings.TrimSpace(source.Overview)
	}
	if target.AirDate == "" {
		target.AirDate = source.AirDate
	}
	if target.VoteAverage == 0 {
		target.VoteAverage = source.VoteAverage
	}
	if target.StillPath == "" {
		target.StillPath = source.StillPath
	}
}

func episodeComplete(episode episodeResponse) bool {
	return episode.ID != 0 && episode.Name != "" && episode.Overview != ""
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
