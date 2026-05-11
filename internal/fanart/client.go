package fanart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
)

var ErrDisabled = errors.New("fanart scraping is disabled")

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type TVImages struct {
	Poster       string
	Fanart       string
	ClearLogo    string
	ClearArt     string
	SeasonPoster string
}

type tvResponse struct {
	ClearLogo      []imageItem       `json:"clearlogo"`
	HDTVLogo       []imageItem       `json:"hdtvlogo"`
	ClearArt       []imageItem       `json:"clearart"`
	HDTVClearArt   []imageItem       `json:"hdclearart"`
	TVPoster       []imageItem       `json:"tvposter"`
	ShowBackground []imageItem       `json:"showbackground"`
	SeasonPoster   []seasonImageItem `json:"seasonposter"`
}

type imageItem struct {
	URL   string `json:"url"`
	Likes string `json:"likes"`
	Lang  string `json:"lang"`
}

type seasonImageItem struct {
	URL    string `json:"url"`
	Likes  string `json:"likes"`
	Lang   string `json:"lang"`
	Season string `json:"season"`
}

func NewClient(cfg config.ScrapingConfig) (*Client, error) {
	if strings.TrimSpace(cfg.FanartAPIKey) == "" {
		return nil, ErrDisabled
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.FanartBaseURL), "/")
	if baseURL == "" {
		baseURL = "https://webservice.fanart.tv/v3"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(cfg.Proxy) != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &Client{baseURL: baseURL, apiKey: strings.TrimSpace(cfg.FanartAPIKey), httpClient: &http.Client{Timeout: 30 * time.Second, Transport: transport}}, nil
}

func (c *Client) GetTVImages(ctx context.Context, tvdbID int, season int, languages []string) (TVImages, error) {
	if tvdbID <= 0 {
		return TVImages{}, errors.New("tvdb id is required for fanart")
	}
	requestURL := fmt.Sprintf("%s/tv/%d?api_key=%s", c.baseURL, tvdbID, url.QueryEscape(c.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return TVImages{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TVImages{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TVImages{}, fmt.Errorf("fanart request failed: %s", resp.Status)
	}
	var parsed tvResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return TVImages{}, err
	}
	return TVImages{
		Poster:       chooseImage(parsed.TVPoster, languages),
		Fanart:       chooseImage(parsed.ShowBackground, languages),
		ClearLogo:    chooseImage(append(parsed.ClearLogo, parsed.HDTVLogo...), languages),
		ClearArt:     chooseImage(append(parsed.ClearArt, parsed.HDTVClearArt...), languages),
		SeasonPoster: chooseSeasonImage(parsed.SeasonPoster, season, languages),
	}, nil
}

func chooseImage(images []imageItem, languages []string) string {
	if len(images) == 0 {
		return ""
	}
	sort.SliceStable(images, func(i int, j int) bool {
		return imageScore(images[i], languages) > imageScore(images[j], languages)
	})
	return strings.TrimSpace(images[0].URL)
}

func chooseSeasonImage(images []seasonImageItem, season int, languages []string) string {
	filtered := make([]imageItem, 0, len(images))
	seasonValue := strconv.Itoa(season)
	for _, image := range images {
		if strings.TrimSpace(image.Season) != seasonValue {
			continue
		}
		filtered = append(filtered, imageItem{URL: image.URL, Likes: image.Likes, Lang: image.Lang})
	}
	return chooseImage(filtered, languages)
}

func imageScore(image imageItem, languages []string) int {
	score := parseInt(image.Likes)
	lang := strings.ToLower(strings.TrimSpace(image.Lang))
	for index, language := range languages {
		if normalizeLanguage(language) == lang {
			score += (len(languages) - index) * 1000
			break
		}
	}
	if lang == "" || lang == "00" {
		score += 100
	}
	return score
}

func normalizeLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if index := strings.Index(language, "-"); index > 0 {
		return language[:index]
	}
	return language
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}
