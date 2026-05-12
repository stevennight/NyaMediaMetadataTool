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
	"sync"
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
	Language string `json:"language"`
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
	TMDBShowID     int    `json:"tmdbShowId"`
	TMDBEpisodeID  int    `json:"tmdbEpisodeId"`
	Source         string `json:"source"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	Conflict       bool   `json:"conflict"`
	SanitizedTitle string `json:"sanitizedTitle"`
	ManualName     bool   `json:"manualName"`
}

type PreviewItemRequest struct {
	Path       string `json:"path"`
	Template   string `json:"template"`
	UseTMDB    bool   `json:"useTmdb"`
	Language   string `json:"language"`
	Show       string `json:"show"`
	Title      string `json:"title"`
	Season     *int   `json:"season"`
	Episode    *int   `json:"episode"`
	TMDBShowID int    `json:"tmdbShowId"`
	NewName    string `json:"newName"`
}

type ApplyRequest struct {
	Items []ApplyItem `json:"items"`
}

type ApplyItem struct {
	Path    string `json:"path"`
	NewName string `json:"newName"`
}

type ApplyResult struct {
	Items []PreviewItem `json:"items"`
}

type episodeNFO struct {
	Title     string `xml:"title"`
	ShowTitle string `xml:"showtitle"`
	Language  string `xml:"language"`
	LangAttr  string `xml:"lang,attr"`
	Season    int    `xml:"season"`
	Episode   int    `xml:"episode"`
	Premiered string `xml:"premiered"`
	Aired     string `xml:"aired"`
}

func Preview(ctx context.Context, cfg config.Config, input PreviewRequest) (PreviewResult, error) {
	items := make([]PreviewItem, 0)
	if err := PreviewEach(ctx, cfg, input, func(item PreviewItem) error {
		items = append(items, item)
		return nil
	}); err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Items: items}, nil
}

func PreviewEach(ctx context.Context, cfg config.Config, input PreviewRequest, emit func(PreviewItem) error) error {
	root := strings.TrimSpace(input.Path)
	if root == "" {
		return errors.New("path is required")
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = DefaultTemplate
	}

	info, err := os.Stat(root)
	if err != nil {
		return err
	}

	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}

	if language := strings.TrimSpace(input.Language); language != "" {
		cfg.Scraping.Language = language
	}
	client, _ := tmdb.NewClient(cfg.Scraping)
	if !input.UseTMDB {
		client = nil
	}

	files := make([]string, 0)
	addFile := func(path string) {
		if _, ok := allowed[strings.ToLower(filepath.Ext(path))]; !ok {
			return
		}
		files = append(files, path)
	}

	if !info.IsDir() {
		addFile(root)
	} else {
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			addFile(path)
			return nil
		})
		if err != nil {
			return err
		}
	}

	workers := previewWorkerCount(cfg.Processing.Concurrency)
	jobs := make(chan string)
	var wg sync.WaitGroup
	var emitMu sync.Mutex
	var firstErr error
	var firstErrMu sync.Mutex

	setErr := func(err error) {
		if err == nil {
			return
		}
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		firstErrMu.Unlock()
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				select {
				case <-ctx.Done():
					setErr(ctx.Err())
					return
				default:
				}

				item := buildItem(ctx, cfg, client, path, template)
				emitMu.Lock()
				err := emit(item)
				emitMu.Unlock()
				if err != nil {
					setErr(err)
					return
				}
			}
		}()
	}

	for _, path := range files {
		firstErrMu.Lock()
		err := firstErr
		firstErrMu.Unlock()
		if err != nil {
			break
		}
		select {
		case <-ctx.Done():
			setErr(ctx.Err())
		case jobs <- path:
		}
	}
	close(jobs)
	wg.Wait()
	return firstErr
}

func PreviewSingle(ctx context.Context, cfg config.Config, input PreviewItemRequest) (PreviewItem, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return PreviewItem{}, errors.New("path is required")
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = DefaultTemplate
	}
	if language := strings.TrimSpace(input.Language); language != "" {
		cfg.Scraping.Language = language
	}

	client, _ := tmdb.NewClient(cfg.Scraping)
	if !input.UseTMDB {
		client = nil
	}

	item := buildItem(ctx, cfg, nil, path, template)
	if strings.TrimSpace(input.Show) != "" {
		item.Show = strings.TrimSpace(input.Show)
	}
	if strings.TrimSpace(input.Title) != "" {
		item.Title = strings.TrimSpace(input.Title)
	}
	if input.Season != nil && *input.Season >= 0 {
		item.Season = *input.Season
	}
	if input.Episode != nil && *input.Episode >= 0 {
		item.Episode = *input.Episode
	}
	if input.TMDBShowID > 0 {
		item.TMDBShowID = input.TMDBShowID
	}

	if client != nil {
		episode, err := findEpisode(ctx, client, item.TMDBShowID, item.Show, item.Season, item.Episode)
		if err != nil {
			item.Status = "warning"
			item.Message = err.Error()
		} else {
			item.Show = firstNonEmpty(episode.ShowName, item.Show)
			item.Title = episode.Title
			item.Year = yearFromDate(episode.AirDate)
			item.TMDBShowID = episode.ShowID
			item.TMDBEpisodeID = episode.EpisodeID
			item.Source = "tmdb"
			item.Status = "ok"
			item.Message = ""
		}
	}

	finalizeItem(path, template, &item)
	if strings.TrimSpace(input.NewName) != "" {
		item.NewName = sanitizeFilenamePart(strings.TrimSuffix(input.NewName, filepath.Ext(input.NewName))) + filepath.Ext(path)
		item.NewPath = filepath.Join(filepath.Dir(path), item.NewName)
		item.ManualName = true
		applyConflict(path, &item)
	}
	return item, nil
}

func Apply(input ApplyRequest) ApplyResult {
	result := ApplyResult{Items: make([]PreviewItem, 0, len(input.Items))}
	for _, entry := range input.Items {
		result.Items = append(result.Items, applyRename(entry))
	}
	return result
}

func previewWorkerCount(configured int) int {
	if configured < 4 {
		return 4
	}
	if configured > 8 {
		return 8
	}
	return configured
}

func applyRename(input ApplyItem) PreviewItem {
	path := strings.TrimSpace(input.Path)
	item := PreviewItem{Path: path, CurrentName: filepath.Base(path), Status: "error"}
	if path == "" {
		item.Message = "path is required"
		return item
	}
	info, err := os.Stat(path)
	if err != nil {
		item.Message = err.Error()
		return item
	}
	if info.IsDir() {
		item.Message = "不支持重命名目录"
		return item
	}
	baseName := strings.TrimSuffix(strings.TrimSpace(input.NewName), filepath.Ext(input.NewName))
	if strings.TrimSpace(baseName) == "" {
		item.Message = "newName is required"
		return item
	}
	name := sanitizeFilenamePart(baseName) + filepath.Ext(path)
	item.NewName = name
	item.NewPath = filepath.Join(filepath.Dir(path), name)
	if samePath(path, item.NewPath) {
		item.Status = "skipped"
		item.Message = "文件名未变化"
		return item
	}
	if _, err := os.Stat(item.NewPath); err == nil {
		item.Conflict = true
		item.Message = "目标文件已存在"
		return item
	}
	if err := os.Rename(path, item.NewPath); err != nil {
		item.Message = err.Error()
		return item
	}
	item.Status = "renamed"
	item.Message = "已重命名"
	item.Path = item.NewPath
	item.CurrentName = item.NewName
	return item
}

func buildItem(ctx context.Context, cfg config.Config, client *tmdb.Client, path string, template string) PreviewItem {
	parsed, ok := parseEpisode(path)
	item := PreviewItem{Path: path, CurrentName: filepath.Base(path), Show: parsed.show, Season: parsed.season, Episode: parsed.episode, Source: "filename", Status: "ok"}
	if !ok {
		item.Status = "warning"
		item.Message = "无法从文件名解析 SxxEyy"
		return item
	}

	if nfo, ok := readEpisodeNFO(path); ok && nfoMatchesLanguage(nfo, cfg.Scraping.Language) {
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
		if episode, err := findEpisode(ctx, client, item.TMDBShowID, item.Show, item.Season, item.Episode); err == nil {
			item.Show = firstNonEmpty(episode.ShowName, item.Show)
			item.Title = episode.Title
			item.Year = yearFromDate(episode.AirDate)
			item.TMDBShowID = episode.ShowID
			item.TMDBEpisodeID = episode.EpisodeID
			item.Source = "tmdb"
		} else {
			item.Status = "warning"
			item.Message = err.Error()
		}
	}

	finalizeItem(path, template, &item)
	return item
}

func findEpisode(ctx context.Context, client *tmdb.Client, tmdbShowID int, show string, season int, episode int) (tmdb.Episode, error) {
	if tmdbShowID > 0 {
		return client.FindEpisodeByShowIDStrictTitle(ctx, tmdbShowID, season, episode)
	}
	return client.FindEpisodeStrictTitle(ctx, show, season, episode)
}

func finalizeItem(path string, template string, item *PreviewItem) {
	if strings.TrimSpace(item.Title) == "" {
		item.Title = titleFromName(path)
	}
	item.SanitizedTitle = sanitizeFilenamePart(item.Title)
	name := applyTemplate(template, *item)
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	name = sanitizeFilenamePart(name) + filepath.Ext(path)
	item.NewName = name
	item.NewPath = filepath.Join(filepath.Dir(path), name)
	applyConflict(path, item)
}

func applyConflict(path string, item *PreviewItem) {
	item.Conflict = false
	if !samePath(path, item.NewPath) {
		if _, err := os.Stat(item.NewPath); err == nil {
			item.Conflict = true
			item.Status = "error"
			item.Message = "目标文件已存在"
		}
	}
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

func nfoMatchesLanguage(nfo episodeNFO, requested string) bool {
	nfoLanguage := firstNonEmpty(nfo.Language, nfo.LangAttr)
	if strings.TrimSpace(nfoLanguage) == "" || strings.TrimSpace(requested) == "" {
		return false
	}
	return normalizeLanguage(nfoLanguage) == normalizeLanguage(requested)
}

func normalizeLanguage(language string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	language = strings.ReplaceAll(language, "_", "-")
	return language
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
