package renamer

import (
	"context"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/tmdb"
)

const DefaultTemplate = "{show} - S{season:00}E{episode:00} - {title}"

var episodePattern = regexp.MustCompile(`(?i)s(\d{1,2})e(\d{1,4})\b`)
var placeholderPattern = regexp.MustCompile(`\{([a-z]+)(?::([^}]+))?\}`)
var reservedNamePattern = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)

type PreviewRequest struct {
	Path     string `json:"path"`
	Template string `json:"template"`
	UseTMDB  bool   `json:"useTmdb"`
}

type PreviewResult struct {
	Items []PreviewItem `json:"items"`
}

type PreviewItem struct {
	Path           string `json:"path"`
	CurrentName    string `json:"currentName"`
	NewName        string `json:"newName"`
	NewPath        string `json:"newPath"`
	Show           string `json:"show"`
	Title          string `json:"title"`
	Season         int    `json:"season"`
	Episode        int    `json:"episode"`
	Year           string `json:"year"`
	Source         string `json:"source"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	Conflict       bool   `json:"conflict"`
	SanitizedTitle string `json:"sanitizedTitle"`
}

type episodeNFO struct {
	Title     string `xml:"title"`
	ShowTitle string `xml:"showtitle"`
	Season    int    `xml:"season"`
	Episode   int    `xml:"episode"`
	Premiered string `xml:"premiered"`
	Aired     string `xml:"aired"`
}

func Preview(ctx context.Context, cfg config.Config, input PreviewRequest) (PreviewResult, error) {
	root := strings.TrimSpace(input.Path)
	if root == "" {
		return PreviewResult{}, errors.New("path is required")
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = DefaultTemplate
	}

	info, err := os.Stat(root)
	if err != nil {
		return PreviewResult{}, err
	}

	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}

	client, _ := tmdb.NewClient(cfg.Scraping)
	if !input.UseTMDB {
		client = nil
	}

	items := make([]PreviewItem, 0)
	addFile := func(path string) {
		if _, ok := allowed[strings.ToLower(filepath.Ext(path))]; !ok {
			return
		}
		items = append(items, buildItem(ctx, cfg, client, path, template))
	}

	if !info.IsDir() {
		addFile(root)
		return PreviewResult{Items: items}, nil
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		addFile(path)
		return nil
	})
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Items: items}, nil
}

func buildItem(ctx context.Context, cfg config.Config, client *tmdb.Client, path string, template string) PreviewItem {
	parsed, ok := parseEpisode(path)
	item := PreviewItem{Path: path, CurrentName: filepath.Base(path), Show: parsed.show, Season: parsed.season, Episode: parsed.episode, Source: "filename", Status: "ok"}
	if !ok {
		item.Status = "warning"
		item.Message = "无法从文件名解析 SxxEyy"
		return item
	}

	if nfo, ok := readEpisodeNFO(path); ok {
		if strings.TrimSpace(nfo.ShowTitle) != "" {
			item.Show = strings.TrimSpace(nfo.ShowTitle)
		}
		if strings.TrimSpace(nfo.Title) != "" {
			item.Title = strings.TrimSpace(nfo.Title)
		}
		if nfo.Season > 0 {
			item.Season = nfo.Season
		}
		if nfo.Episode > 0 {
			item.Episode = nfo.Episode
		}
		item.Year = yearFromDate(firstNonEmpty(nfo.Premiered, nfo.Aired))
		item.Source = "nfo"
	} else if client != nil {
		if episode, err := client.FindEpisode(ctx, item.Show, item.Season, item.Episode); err == nil {
			item.Show = firstNonEmpty(episode.ShowName, item.Show)
			item.Title = episode.Title
			item.Year = yearFromDate(episode.AirDate)
			item.Source = "tmdb"
		} else {
			item.Status = "warning"
			item.Message = err.Error()
		}
	}

	if strings.TrimSpace(item.Title) == "" {
		item.Title = titleFromName(path)
	}
	item.SanitizedTitle = sanitizeFilenamePart(item.Title)

	name := applyTemplate(template, item)
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	name = sanitizeFilenamePart(name) + filepath.Ext(path)
	item.NewName = name
	item.NewPath = filepath.Join(filepath.Dir(path), name)
	if !samePath(path, item.NewPath) {
		if _, err := os.Stat(item.NewPath); err == nil {
			item.Conflict = true
			item.Status = "error"
			item.Message = "目标文件已存在"
		}
	}
	return item
}

type parsedEpisode struct {
	show    string
	season  int
	episode int
}

func parseEpisode(path string) (parsedEpisode, bool) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	match := episodePattern.FindStringSubmatch(name)
	if len(match) != 3 {
		return parsedEpisode{}, false
	}
	season, err := strconv.Atoi(match[1])
	if err != nil {
		return parsedEpisode{}, false
	}
	episode, err := strconv.Atoi(match[2])
	if err != nil {
		return parsedEpisode{}, false
	}
	show := strings.TrimSpace(name[:strings.Index(strings.ToLower(name), strings.ToLower(match[0]))])
	show = cleanQuery(show)
	if show == "" {
		show = cleanQuery(filepath.Base(filepath.Dir(path)))
	}
	return parsedEpisode{show: show, season: season, episode: episode}, true
}

func readEpisodeNFO(mediaPath string) (episodeNFO, bool) {
	path := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".nfo"
	data, err := os.ReadFile(path)
	if err != nil {
		return episodeNFO{}, false
	}
	var doc episodeNFO
	if err := xml.Unmarshal(data, &doc); err != nil {
		return episodeNFO{}, false
	}
	return doc, true
}

func applyTemplate(template string, item PreviewItem) string {
	values := map[string]string{
		"show":    item.Show,
		"season":  strconv.Itoa(item.Season),
		"episode": strconv.Itoa(item.Episode),
		"title":   item.Title,
		"year":    item.Year,
	}
	return placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := placeholderPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return ""
		}
		value := values[parts[1]]
		if len(parts) >= 3 && strings.Trim(parts[2], "0") == "" && strings.TrimSpace(parts[2]) != "" {
			if number, err := strconv.Atoi(value); err == nil {
				return leftPad(strconv.Itoa(number), len(parts[2]))
			}
		}
		return value
	})
}

func sanitizeFilenamePart(value string) string {
	value = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, " .")
	if value == "" {
		value = "Untitled"
	}
	if reservedNamePattern.MatchString(value) {
		value = "_" + value
	}
	if len([]rune(value)) > 180 {
		value = string([]rune(value)[:180])
		value = strings.Trim(value, " .")
	}
	return value
}

func titleFromName(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	match := episodePattern.FindStringIndex(name)
	if len(match) == 2 && match[1] < len(name) {
		return cleanQuery(name[match[1]:])
	}
	return name
}

func cleanQuery(value string) string {
	value = strings.Trim(value, " .-_[]()")
	value = strings.ReplaceAll(value, ".", " ")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	return strings.Join(strings.Fields(value), " ")
}

func yearFromDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 4 {
		return value[:4]
	}
	return ""
}

func leftPad(value string, width int) string {
	for len(value) < width {
		value = "0" + value
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func samePath(a string, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return strings.EqualFold(absA, absB)
	}
	return strings.EqualFold(a, b)
}
