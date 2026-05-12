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
	ShowID           int
	EpisodeID        int
	ShowName         string
	ShowFirstAirDate string
	Title            string
	Overview         string
	AirDate          string
	VoteAverage      float64
	StillPath        string
	EpisodeGroup     string
	Actors           []Actor
	Crew             []CrewMember
}

type Show struct {
	ID               int
	TVDBID           int
	Name             string
	OriginalLanguage string
	Overview         string
	FirstAirDate     string
	Status           string
	VoteAverage      float64
	Genres           []string
	PosterPath       string
	BackdropPath     string
	LogoPath         string
}

type SearchResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	FirstAirDate string `json:"firstAirDate"`
	Overview     string `json:"overview"`
}

type Season struct {
	ID           int
	Name         string
	Overview     string
	AirDate      string
	SeasonNumber int
	EpisodeCount int
	PosterPath   string
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

type showResponse struct {
	ID               int           `json:"id"`
	Name             string        `json:"name"`
	OriginalName     string        `json:"original_name"`
	OriginalLanguage string        `json:"original_language"`
	Overview         string        `json:"overview"`
	FirstAirDate     string        `json:"first_air_date"`
	Status           string        `json:"status"`
	VoteAverage      float64       `json:"vote_average"`
	Genres           []genreResult `json:"genres"`
}

type genreResult struct {
	Name string `json:"name"`
}

type seasonResponse struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	AirDate      string `json:"air_date"`
	SeasonNumber int    `json:"season_number"`
	Episodes     []any  `json:"episodes"`
}

type imageResponse struct {
	Posters   []imageItem `json:"posters"`
	Backdrops []imageItem `json:"backdrops"`
	Logos     []imageItem `json:"logos"`
}

type imageItem struct {
	FilePath      string  `json:"file_path"`
	Iso6391       string  `json:"iso_639_1"`
	VoteAverage   float64 `json:"vote_average"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	LanguageMatch string  `json:"-"`
}

type externalIDsResponse struct {
	TVDBID int `json:"tvdb_id"`
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

func (c *Client) SearchTV(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("tmdb show query is empty")
	}

	var parsed searchTVResponse
	if err := c.get(ctx, c.language, "/search/tv", url.Values{"query": {query}}, &parsed); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(parsed.Results))
	for _, result := range parsed.Results {
		results = append(results, SearchResult{
			ID:           result.ID,
			Name:         result.Name,
			OriginalName: result.OriginalName,
			FirstAirDate: result.FirstAirDate,
			Overview:     result.Overview,
		})
	}
	return results, nil
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
		ShowID:           show.ID,
		EpisodeID:        episodeDetail.ID,
		ShowName:         firstNonEmpty(show.Name, show.OriginalName),
		ShowFirstAirDate: show.FirstAirDate,
		Title:            episodeDetail.Name,
		Overview:         episodeDetail.Overview,
		AirDate:          episodeDetail.AirDate,
		VoteAverage:      episodeDetail.VoteAverage,
		StillPath:        episodeDetail.StillPath,
		Actors:           credits.Actors,
		Crew:             credits.Crew,
	}, nil
}

func (c *Client) FindEpisodeByShowID(ctx context.Context, showID int, season int, episode int) (Episode, error) {
	if showID == 0 {
		return Episode{}, errors.New("tmdb show id is empty")
	}

	showDetail, err := c.getShowWithFallback(ctx, showID)
	if err != nil {
		return Episode{}, err
	}
	episodeDetail, err := c.getEpisodeWithFallback(ctx, showID, season, episode)
	if err != nil {
		return Episode{}, err
	}
	var credits episodeCredits
	if c.people {
		credits, err = c.getEpisodeCredits(ctx, showID, season, episode)
		if err != nil {
			return Episode{}, err
		}
	}

	return Episode{
		ShowID:           showID,
		EpisodeID:        episodeDetail.ID,
		ShowName:         firstNonEmpty(showDetail.Name, showDetail.OriginalName),
		ShowFirstAirDate: showDetail.FirstAirDate,
		Title:            episodeDetail.Name,
		Overview:         episodeDetail.Overview,
		AirDate:          episodeDetail.AirDate,
		VoteAverage:      episodeDetail.VoteAverage,
		StillPath:        episodeDetail.StillPath,
		Actors:           credits.Actors,
		Crew:             credits.Crew,
	}, nil
}

func (c *Client) FindEpisodeStrictTitle(ctx context.Context, showQuery string, season int, episode int) (Episode, error) {
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

	episodeDetail, err := c.getEpisode(ctx, c.language, show.ID, season, episode)
	if err != nil {
		return Episode{}, err
	}
	return c.episodeFromDetails(ctx, show.ID, firstNonEmpty(show.Name, show.OriginalName), show.FirstAirDate, episodeDetail, season, episode)
}

func (c *Client) FindEpisodeByShowIDStrictTitle(ctx context.Context, showID int, season int, episode int) (Episode, error) {
	if showID == 0 {
		return Episode{}, errors.New("tmdb show id is empty")
	}

	showDetail, err := c.getShow(ctx, c.language, showID)
	if err != nil {
		return Episode{}, err
	}
	episodeDetail, err := c.getEpisode(ctx, c.language, showID, season, episode)
	if err != nil {
		return Episode{}, err
	}
	return c.episodeFromDetails(ctx, showID, firstNonEmpty(showDetail.Name, showDetail.OriginalName), showDetail.FirstAirDate, episodeDetail, season, episode)
}

func (c *Client) episodeFromDetails(ctx context.Context, showID int, showName string, showFirstAirDate string, detail episodeResponse, season int, episode int) (Episode, error) {
	var credits episodeCredits
	var err error
	if c.people {
		credits, err = c.getEpisodeCredits(ctx, showID, season, episode)
		if err != nil {
			return Episode{}, err
		}
	}

	return Episode{
		ShowID:           showID,
		EpisodeID:        detail.ID,
		ShowName:         showName,
		ShowFirstAirDate: showFirstAirDate,
		Title:            detail.Name,
		Overview:         detail.Overview,
		AirDate:          detail.AirDate,
		VoteAverage:      detail.VoteAverage,
		StillPath:        detail.StillPath,
		Actors:           credits.Actors,
		Crew:             credits.Crew,
	}, nil
}

func (c *Client) FindShowAndSeason(ctx context.Context, showQuery string, season int) (Show, Season, error) {
	showQuery = strings.TrimSpace(showQuery)
	if showQuery == "" {
		return Show{}, Season{}, errors.New("tmdb show query is empty")
	}

	showMatch, err := c.searchTV(ctx, showQuery)
	if err != nil {
		return Show{}, Season{}, err
	}
	if showMatch.ID == 0 {
		return Show{}, Season{}, fmt.Errorf("tmdb tv show not found: %s", showQuery)
	}

	showDetail, err := c.getShowWithFallback(ctx, showMatch.ID)
	if err != nil {
		return Show{}, Season{}, err
	}
	seasonDetail, err := c.getSeasonWithFallback(ctx, showMatch.ID, season)
	if err != nil {
		return Show{}, Season{}, err
	}

	genres := make([]string, 0, len(showDetail.Genres))
	for _, genre := range showDetail.Genres {
		if strings.TrimSpace(genre.Name) != "" {
			genres = append(genres, strings.TrimSpace(genre.Name))
		}
	}

	return Show{
			ID:               showDetail.ID,
			Name:             firstNonEmpty(showDetail.Name, showDetail.OriginalName, showMatch.Name, showMatch.OriginalName),
			OriginalLanguage: showDetail.OriginalLanguage,
			Overview:         showDetail.Overview,
			FirstAirDate:     showDetail.FirstAirDate,
			Status:           showDetail.Status,
			VoteAverage:      showDetail.VoteAverage,
			Genres:           genres,
		}, Season{
			ID:           seasonDetail.ID,
			Name:         seasonDetail.Name,
			Overview:     seasonDetail.Overview,
			AirDate:      seasonDetail.AirDate,
			SeasonNumber: seasonDetail.SeasonNumber,
			EpisodeCount: len(seasonDetail.Episodes),
		}, nil
}

func (c *Client) FindShowAndSeasonImages(ctx context.Context, showQuery string, season int, preferOriginalLanguagePoster bool) (Show, Season, error) {
	show, seasonDetail, err := c.FindShowAndSeason(ctx, showQuery, season)
	if err != nil {
		return Show{}, Season{}, err
	}

	languagePriority := c.imageLanguagePriority(show.OriginalLanguage, preferOriginalLanguagePoster)
	showImages, err := c.getShowImages(ctx, show.ID, languagePriority)
	if err != nil {
		return Show{}, Season{}, err
	}
	seasonImages, err := c.getSeasonImages(ctx, show.ID, season, languagePriority)
	if err != nil {
		return Show{}, Season{}, err
	}

	show.PosterPath = chooseImage(showImages.Posters, languagePriority, false)
	show.BackdropPath = chooseImage(showImages.Backdrops, languagePriority, true)
	show.LogoPath = chooseImage(filterImageExt(showImages.Logos, ".png"), languagePriority, false)
	show.TVDBID = c.getTVDBID(ctx, show.ID)
	seasonDetail.PosterPath = chooseImage(seasonImages.Posters, languagePriority, false)
	return show, seasonDetail, nil
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

func (c *Client) getShowWithFallback(ctx context.Context, showID int) (showResponse, error) {
	var merged showResponse
	var firstErr error
	for _, language := range c.languages {
		detail, err := c.getShow(ctx, language, showID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		mergeShow(&merged, detail)
		if showComplete(merged) {
			return merged, nil
		}
	}
	if merged.ID != 0 {
		return merged, nil
	}
	if firstErr != nil {
		return showResponse{}, firstErr
	}
	return showResponse{}, errors.New("tmdb show not found")
}

func (c *Client) getSeasonWithFallback(ctx context.Context, showID int, season int) (seasonResponse, error) {
	var merged seasonResponse
	var firstErr error
	for _, language := range c.languages {
		detail, err := c.getSeason(ctx, language, showID, season)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		mergeSeason(&merged, detail)
		if seasonComplete(merged) {
			return merged, nil
		}
	}
	if merged.ID != 0 {
		return merged, nil
	}
	if firstErr != nil {
		return seasonResponse{}, firstErr
	}
	return seasonResponse{}, errors.New("tmdb season not found")
}

func (c *Client) getShow(ctx context.Context, language string, showID int) (showResponse, error) {
	var parsed showResponse
	path := fmt.Sprintf("/tv/%d", showID)
	if err := c.get(ctx, language, path, nil, &parsed); err != nil {
		return showResponse{}, err
	}
	return parsed, nil
}

func (c *Client) getSeason(ctx context.Context, language string, showID int, season int) (seasonResponse, error) {
	var parsed seasonResponse
	path := fmt.Sprintf("/tv/%d/season/%d", showID, season)
	if err := c.get(ctx, language, path, nil, &parsed); err != nil {
		return seasonResponse{}, err
	}
	return parsed, nil
}

func (c *Client) getShowImages(ctx context.Context, showID int, languages []string) (imageResponse, error) {
	var parsed imageResponse
	path := fmt.Sprintf("/tv/%d/images", showID)
	query := url.Values{"include_image_language": {imageLanguageQuery(languages)}}
	if err := c.get(ctx, c.language, path, query, &parsed); err != nil {
		return imageResponse{}, err
	}
	return parsed, nil
}

func (c *Client) getSeasonImages(ctx context.Context, showID int, season int, languages []string) (imageResponse, error) {
	var parsed imageResponse
	path := fmt.Sprintf("/tv/%d/season/%d/images", showID, season)
	query := url.Values{"include_image_language": {imageLanguageQuery(languages)}}
	if err := c.get(ctx, c.language, path, query, &parsed); err != nil {
		return imageResponse{}, err
	}
	return parsed, nil
}

func (c *Client) getTVDBID(ctx context.Context, showID int) int {
	var parsed externalIDsResponse
	path := fmt.Sprintf("/tv/%d/external_ids", showID)
	if err := c.get(ctx, c.language, path, nil, &parsed); err != nil {
		return 0
	}
	return parsed.TVDBID
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

	resp, err := c.doWithRetry(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tmdb request failed: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}

		clone := req.Clone(req.Context())
		resp, err := c.httpClient.Do(clone)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
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

func mergeShow(target *showResponse, source showResponse) {
	if target.ID == 0 {
		target.ID = source.ID
	}
	if target.Name == "" {
		target.Name = strings.TrimSpace(source.Name)
	}
	if target.OriginalName == "" {
		target.OriginalName = strings.TrimSpace(source.OriginalName)
	}
	if target.OriginalLanguage == "" {
		target.OriginalLanguage = strings.TrimSpace(source.OriginalLanguage)
	}
	if target.Overview == "" {
		target.Overview = strings.TrimSpace(source.Overview)
	}
	if target.FirstAirDate == "" {
		target.FirstAirDate = source.FirstAirDate
	}
	if target.Status == "" {
		target.Status = source.Status
	}
	if target.VoteAverage == 0 {
		target.VoteAverage = source.VoteAverage
	}
	if len(target.Genres) == 0 {
		target.Genres = source.Genres
	}
}

func mergeSeason(target *seasonResponse, source seasonResponse) {
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
	if target.SeasonNumber == 0 {
		target.SeasonNumber = source.SeasonNumber
	}
	if len(target.Episodes) == 0 {
		target.Episodes = source.Episodes
	}
}

func episodeComplete(episode episodeResponse) bool {
	return episode.ID != 0 && episode.Name != "" && episode.Overview != ""
}

func showComplete(show showResponse) bool {
	return show.ID != 0 && show.Name != "" && show.Overview != ""
}

func seasonComplete(season seasonResponse) bool {
	return season.ID != 0 && season.Name != "" && season.Overview != ""
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

func (c *Client) imageLanguagePriority(originalLanguage string, preferOriginal bool) []string {
	result := []string{}
	seen := map[string]struct{}{}
	appendLanguage := func(language string) {
		language = normalizeImageLanguage(language)
		if language == "" {
			return
		}
		if _, ok := seen[language]; ok {
			return
		}
		seen[language] = struct{}{}
		result = append(result, language)
	}
	if preferOriginal {
		appendLanguage(originalLanguage)
	}
	for _, language := range c.languages {
		appendLanguage(language)
	}
	if !preferOriginal {
		appendLanguage(originalLanguage)
	}
	result = append(result, "")
	return result
}

func imageLanguageQuery(languages []string) string {
	values := make([]string, 0, len(languages)+1)
	seen := map[string]struct{}{}
	for _, language := range languages {
		language = normalizeImageLanguage(language)
		if language == "" {
			continue
		}
		if _, ok := seen[language]; ok {
			continue
		}
		seen[language] = struct{}{}
		values = append(values, language)
	}
	values = append(values, "null")
	return strings.Join(values, ",")
}

func normalizeImageLanguage(language string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	if language == "" || language == "null" {
		return ""
	}
	if index := strings.Index(language, "-"); index > 0 {
		return language[:index]
	}
	return language
}

func chooseImage(images []imageItem, languagePriority []string, landscape bool) string {
	bestScore := -1.0
	bestPath := ""
	for _, image := range images {
		if strings.TrimSpace(image.FilePath) == "" {
			continue
		}
		imageLanguage := normalizeImageLanguage(image.Iso6391)
		languageScore := float64(len(languagePriority) + 1)
		matched := false
		for index, language := range languagePriority {
			if language == imageLanguage {
				languageScore = float64(len(languagePriority) - index)
				matched = true
				break
			}
		}
		if !matched && imageLanguage != "" {
			languageScore = 0
		}
		dimensionScore := 0.0
		if landscape && image.Width > image.Height {
			dimensionScore = 0.5
		}
		if !landscape && image.Height >= image.Width {
			dimensionScore = 0.5
		}
		score := languageScore*100 + image.VoteAverage + dimensionScore
		if score > bestScore {
			bestScore = score
			bestPath = image.FilePath
		}
	}
	return bestPath
}

func filterImageExt(images []imageItem, ext string) []imageItem {
	result := make([]imageItem, 0, len(images))
	for _, image := range images {
		if strings.EqualFold(filepathExt(image.FilePath), ext) {
			result = append(result, image)
		}
	}
	return result
}

func filepathExt(path string) string {
	if index := strings.LastIndex(path, "."); index >= 0 {
		return path[index:]
	}
	return ""
}
