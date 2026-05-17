package renamer

import (
	"context"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/episodeparse"
	"NyaMediaMetadataTool/internal/tmdb"
)

const DefaultTemplate = "{show} - S{season:00}E{episode:00} - {title}"

var episodePattern = regexp.MustCompile(`(?i)s(\d{1,2})e(\d{1,4})\b`)
var seasonDirPattern = regexp.MustCompile(`(?i)^(season\s*\d{1,2}|s\d{1,2}|第\s*\d{1,2}\s*季)$`)
var tmdbIDPattern = regexp.MustCompile(`(?i)[\[{(]\s*(?:tmdb(?:id)?|tmid)\s*[-=: ]\s*(\d+)\s*[\]})]`)
var directoryYearPattern = regexp.MustCompile(`[\[{(]\s*(?:19|20)\d{2}\s*[\]})]`)
var leadingReleaseGroupPattern = regexp.MustCompile(`^\s*\[[^\]]+\]\s*`)
var trailingMediaTagPattern = regexp.MustCompile(`(?i)\s*[\[{(][^\]})]*(?:\d{3,4}\s*[x×]\s*\d{3,4}|\d{3,4}p|x264|x265|h\.?(?:264|265)|hevc|avc|aac|flac|opus|mkv|mp4|sub|chs|cht|jap)[^\]})]*[\]})]\s*$`)
var placeholderPattern = regexp.MustCompile(`\{([A-Za-z]+)(?::([^}]+))?\}`)
var reservedNamePattern = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)

const ignoreFileName = ".ignore"

var sidecarExtensions = map[string]struct{}{
	".nfo":  {},
	".srt":  {},
	".ass":  {},
	".ssa":  {},
	".vtt":  {},
	".json": {},
	".bif":  {},
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".webp": {},
}

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
	Path            string `json:"path"`
	CurrentName     string `json:"currentName"`
	NewName         string `json:"newName"`
	NewPath         string `json:"newPath"`
	RenderedTarget  string `json:"renderedTarget"`
	Show            string `json:"show"`
	ShowOriginal    string `json:"showOriginal"`
	ReleaseGroup    string `json:"releaseGroup"`
	Title           string `json:"title"`
	Season          int    `json:"season"`
	Episode         int    `json:"episode"`
	Year            string `json:"year"`
	TMDBShowID      int    `json:"tmdbShowId"`
	TMDBEpisodeID   int    `json:"tmdbEpisodeId"`
	Source          string `json:"source"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	Conflict        bool   `json:"conflict"`
	SanitizedTitle  string `json:"sanitizedTitle"`
	ManualName      bool   `json:"manualName"`
	showByLanguage  map[string]string
	titleByLanguage map[string]string
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
	NewPath string `json:"newPath"`
}

type ApplyResult struct {
	BatchID string        `json:"batchId"`
	Items   []PreviewItem `json:"items"`
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
	ignoreRoot := root
	if !info.IsDir() {
		ignoreRoot = filepath.Dir(root)
	}
	if hasIgnoreFileInAncestors(ignoreRoot) {
		return nil
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
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if hasIgnoreFile(path) {
					return filepath.SkipDir
				}
				return nil
			}
			addFile(path)
			return nil
		})
		if err != nil {
			return err
		}
	}

	sort.SliceStable(files, func(i int, j int) bool {
		left := strings.ToLower(filepath.Base(files[i]))
		right := strings.ToLower(filepath.Base(files[j]))
		if left == right {
			return strings.ToLower(files[i]) < strings.ToLower(files[j])
		}
		return left < right
	})

	type previewJob struct {
		index int
		path  string
	}
	type previewResult struct {
		index int
		item  PreviewItem
	}

	workers := previewWorkerCount(cfg.Processing.Concurrency)
	jobs := make(chan previewJob)
	results := make(chan previewResult)
	var wg sync.WaitGroup
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
			for job := range jobs {
				select {
				case <-ctx.Done():
					setErr(ctx.Err())
					return
				default:
				}

				item := buildItem(ctx, cfg, client, job.path, template)
				select {
				case <-ctx.Done():
					setErr(ctx.Err())
					return
				case results <- previewResult{index: job.index, item: item}:
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index, path := range files {
			firstErrMu.Lock()
			err := firstErr
			firstErrMu.Unlock()
			if err != nil {
				return
			}
			select {
			case <-ctx.Done():
				setErr(ctx.Err())
				return
			case jobs <- previewJob{index: index, path: path}:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	next := 0
	pending := make(map[int]PreviewItem)
	for result := range results {
		pending[result.index] = result.item
		for {
			item, ok := pending[next]
			if !ok {
				break
			}
			if err := emit(item); err != nil {
				setErr(err)
			}
			delete(pending, next)
			next++
		}
	}
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
		episode, err := findEpisode(ctx, client, item.TMDBShowID, item.Show, item.Year, item.Season, item.Episode)
		if err != nil {
			item.Status = "warning"
			item.Message = err.Error()
		} else {
			item.Show = firstNonEmpty(episode.ShowName, item.Show)
			item.ShowOriginal = episode.ShowOriginalName
			item.Title = episode.Title
			item.Year = yearFromDate(firstNonEmpty(episode.ShowFirstAirDate, episode.AirDate))
			item.TMDBShowID = episode.ShowID
			item.TMDBEpisodeID = episode.EpisodeID
			item.Source = "tmdb"
			item.Status = "ok"
			item.Message = ""
		}
		applyLocalizedTemplateValues(ctx, client, template, &item)
	}

	finalizeItem(path, template, &item)
	if strings.TrimSpace(input.NewName) != "" {
		item.RenderedTarget = strings.TrimSpace(input.NewName)
		item.NewPath = targetPathFromTemplate(path, input.NewName)
		item.NewName = filepath.Base(item.NewPath)
		item.ManualName = true
		applyConflict(path, &item)
	}
	return item, nil
}

func Apply(historyPath string, input ApplyRequest) (ApplyResult, error) {
	batch := HistoryBatch{ID: strconv.FormatInt(timeNowUnixNano(), 36), CreatedAt: nowRFC3339()}
	result := ApplyResult{BatchID: batch.ID, Items: make([]PreviewItem, 0, len(input.Items))}
	for _, entry := range input.Items {
		item, moves := applyRename(entry)
		result.Items = append(result.Items, item)
		if len(moves) > 0 {
			batch.Items = append(batch.Items, HistoryItem{Path: entry.Path, NewPath: item.NewPath, Status: item.Status, Message: item.Message, Moves: moves})
		}
	}
	if err := appendHistoryBatch(historyPath, batch); err != nil {
		return result, err
	}
	return result, nil
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

func hasIgnoreFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ignoreFileName))
	return err == nil
}

func hasIgnoreFileInAncestors(dir string) bool {
	for {
		if hasIgnoreFile(dir) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func applyRename(input ApplyItem) (PreviewItem, []RenameMove) {
	moves := make([]RenameMove, 0)
	path := strings.TrimSpace(input.Path)
	item := PreviewItem{Path: path, CurrentName: filepath.Base(path), Status: "error"}
	if path == "" {
		item.Message = "path is required"
		return item, moves
	}
	info, err := os.Stat(path)
	if err != nil {
		item.Message = err.Error()
		return item, moves
	}
	if info.IsDir() {
		item.Message = "不支持重命名目录"
		return item, moves
	}
	targetValue := firstNonEmpty(input.NewPath, input.NewName)
	target := targetPathFromTemplate(path, targetValue)
	if strings.TrimSpace(target) == "" {
		item.Message = "newName is required"
		return item, moves
	}
	item.NewPath = target
	item.NewName = filepath.Base(target)
	sidecars, err := sidecarRenames(path, item.NewPath)
	if err != nil {
		item.Message = err.Error()
		return item, moves
	}
	if samePath(path, item.NewPath) {
		item.Status = "skipped"
		item.Message = "文件名未变化"
		return item, moves
	}
	if _, err := os.Stat(item.NewPath); err == nil {
		item.Conflict = true
		item.Message = "目标文件已存在"
		return item, moves
	}
	for _, sidecar := range sidecars {
		if _, err := os.Stat(sidecar.To); err == nil && !samePath(sidecar.From, sidecar.To) {
			item.Conflict = true
			item.Message = "附属文件目标已存在: " + filepath.Base(sidecar.To)
			return item, moves
		}
	}
	if err := os.MkdirAll(filepath.Dir(item.NewPath), 0o755); err != nil {
		item.Message = err.Error()
		return item, moves
	}
	for _, sidecar := range sidecars {
		if err := os.MkdirAll(filepath.Dir(sidecar.To), 0o755); err != nil {
			item.Message = err.Error()
			return item, moves
		}
	}
	if err := os.Rename(path, item.NewPath); err != nil {
		item.Message = err.Error()
		return item, moves
	}
	moves = append(moves, RenameMove{From: path, To: item.NewPath})
	if _, err := os.Stat(item.NewPath); err != nil {
		item.Status = "warning"
		item.Message = "系统报告已重命名，但目标文件不可访问: " + err.Error()
		return item, moves
	}
	renamedSidecars := 0
	for _, sidecar := range sidecars {
		if samePath(sidecar.From, sidecar.To) {
			continue
		}
		if err := os.Rename(sidecar.From, sidecar.To); err != nil {
			item.Status = "warning"
			item.Message = "媒体文件已重命名，附属文件失败: " + err.Error()
			item.Path = item.NewPath
			item.CurrentName = item.NewName
			return item, moves
		}
		moves = append(moves, RenameMove{From: sidecar.From, To: sidecar.To})
		renamedSidecars++
		updateRenamedNFOReferences(sidecar.To, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), strings.TrimSuffix(filepath.Base(item.NewPath), filepath.Ext(item.NewPath)))
	}
	item.Status = "renamed"
	item.Message = "已重命名"
	if renamedSidecars > 0 {
		item.Message = "已重命名，附属文件 " + strconv.Itoa(renamedSidecars) + " 个"
	}
	item.Path = item.NewPath
	item.CurrentName = item.NewName
	return item, moves
}

func nowRFC3339() string { return time.Now().Format(time.RFC3339) }

func timeNowUnixNano() int64 { return time.Now().UnixNano() }

type sidecarRename struct {
	From string
	To   string
}

func sidecarRenames(mediaPath string, newMediaPath string) ([]sidecarRename, error) {
	dir := filepath.Dir(mediaPath)
	targetDir := filepath.Dir(newMediaPath)
	oldBase := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	newBase := strings.TrimSuffix(filepath.Base(newMediaPath), filepath.Ext(newMediaPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	renames := make([]sidecarRename, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == filepath.Base(mediaPath) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := sidecarExtensions[ext]; !ok {
			continue
		}
		if !strings.HasPrefix(name, oldBase+".") && !strings.HasPrefix(name, oldBase+"-") {
			continue
		}
		nextName := newBase + strings.TrimPrefix(name, oldBase)
		renames = append(renames, sidecarRename{From: filepath.Join(dir, name), To: filepath.Join(targetDir, nextName)})
	}
	return renames, nil
}

func updateRenamedNFOReferences(path string, oldBase string, newBase string) {
	if !strings.EqualFold(filepath.Ext(path), ".nfo") {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	content = strings.ReplaceAll(content, oldBase+"-", newBase+"-")
	content = strings.ReplaceAll(content, oldBase+".", newBase+".")
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func buildItem(ctx context.Context, cfg config.Config, client *tmdb.Client, path string, template string) PreviewItem {
	parsed, ok := parseEpisode(path, cfg)
	item := PreviewItem{Path: path, CurrentName: filepath.Base(path), Show: parsed.show, ReleaseGroup: parsed.releaseGroup, Year: parsed.year, TMDBShowID: parsed.tmdbShowID, Season: parsed.season, Episode: parsed.episode, Source: "filename", Status: "ok"}
	if !ok {
		item.Status = "warning"
		item.Message = "无法从文件名解析集数"
		return item
	}

	matchedNFO := false
	if nfo, ok := readEpisodeNFO(path); ok && nfoMatchesLanguage(nfo, cfg.Scraping.Language) {
		matchedNFO = true
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
	}
	if client != nil {
		if episode, err := findEpisode(ctx, client, item.TMDBShowID, item.Show, item.Year, item.Season, item.Episode); err == nil {
			if !matchedNFO {
				item.Show = firstNonEmpty(episode.ShowName, item.Show)
				item.ShowOriginal = episode.ShowOriginalName
				item.Title = episode.Title
				item.Source = "tmdb"
			}
			item.Year = yearFromDate(firstNonEmpty(episode.ShowFirstAirDate, episode.AirDate))
			item.TMDBShowID = episode.ShowID
			item.TMDBEpisodeID = episode.EpisodeID
			if strings.TrimSpace(item.ShowOriginal) == "" {
				item.ShowOriginal = episode.ShowOriginalName
			}
		} else if !matchedNFO {
			item.Status = "warning"
			item.Message = err.Error()
		}
		applyLocalizedTemplateValues(ctx, client, template, &item)
	}

	finalizeItem(path, template, &item)
	return item
}

func findEpisode(ctx context.Context, client *tmdb.Client, tmdbShowID int, show string, year string, season int, episode int) (tmdb.Episode, error) {
	if tmdbShowID > 0 {
		return client.FindEpisodeByShowID(ctx, tmdbShowID, season, episode)
	}
	if strings.TrimSpace(year) != "" {
		return client.FindEpisodeByYear(ctx, show, year, season, episode)
	}
	return client.FindEpisode(ctx, show, season, episode)
}

func finalizeItem(path string, template string, item *PreviewItem) {
	if strings.TrimSpace(item.Title) == "" {
		item.Title = titleFromName(path)
	}
	item.SanitizedTitle = sanitizeFilenamePart(item.Title)
	rendered := applyTemplate(template, *item)
	if strings.TrimSpace(rendered) == "" {
		rendered = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	item.RenderedTarget = strings.TrimSpace(rendered)
	item.NewPath = targetPathFromTemplate(path, rendered)
	item.NewName = filepath.Base(item.NewPath)
	applyConflict(path, item)
}

func targetPathFromTemplate(sourcePath string, rendered string) string {
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return ""
	}
	sourceExt := filepath.Ext(sourcePath)
	if strings.EqualFold(filepath.Ext(rendered), sourceExt) {
		rendered = strings.TrimSuffix(rendered, filepath.Ext(rendered))
	}
	rendered = sanitizePath(rendered) + sourceExt
	if isAbsoluteTargetPath(rendered) {
		return filepath.Clean(rendered)
	}
	return filepath.Join(filepath.Dir(sourcePath), rendered)
}

func sanitizePath(value string) string {
	volume := volumeName(value)
	rest := strings.TrimPrefix(value, volume)
	separatorPrefix := ""
	for strings.HasPrefix(rest, `\`) || strings.HasPrefix(rest, `/`) {
		separatorPrefix += string(os.PathSeparator)
		rest = rest[1:]
	}
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' })
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeFilenamePart(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	joined := filepath.Join(cleaned...)
	if joined == "." {
		joined = ""
	}
	return volume + separatorPrefix + joined
}

func isAbsoluteTargetPath(value string) bool {
	if filepath.IsAbs(value) {
		return true
	}
	return len(value) >= 3 && isWindowsDriveLetter(value[0]) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func volumeName(value string) string {
	if volume := filepath.VolumeName(value); volume != "" {
		return volume
	}
	if len(value) >= 2 && isWindowsDriveLetter(value[0]) && value[1] == ':' {
		return value[:2]
	}
	return ""
}

func isWindowsDriveLetter(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
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
	show         string
	releaseGroup string
	year         string
	tmdbShowID   int
	season       int
	episode      int
}

func parseEpisode(path string, cfg config.Config) (parsedEpisode, bool) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parsed, ok := episodeparse.Parse(name, cfg.Processing.EpisodePatterns)
	if !ok {
		return parsedEpisode{}, false
	}
	showDir := showDirectory(path)
	releaseGroup := parseReleaseGroup(name)
	show := parseShowName(path, name, parsed.Token)
	if show == "" {
		show = cleanTMDBQuery(filepath.Base(showDir))
	}
	return parsedEpisode{show: show, releaseGroup: releaseGroup, year: parseDirectoryYearFromPath(showDir), tmdbShowID: parseTMDBShowIDFromPath(showDir), season: parsed.Season, episode: parsed.Episode}, true
}

func parseReleaseGroup(fileTitle string) string {
	match := leadingReleaseGroupPattern.FindStringSubmatch(fileTitle)
	if len(match) == 0 {
		return ""
	}
	return strings.Trim(match[0], " []")
}

func parseShowName(path string, fileTitle string, episodeToken string) string {
	show := strings.TrimSpace(fileTitle)
	if episodeToken != "" {
		index := strings.Index(strings.ToLower(show), strings.ToLower(episodeToken))
		if index >= 0 {
			show = show[:index]
		}
	}
	show = cleanTMDBQuery(show)
	if show != "" {
		return show
	}
	return cleanTMDBQuery(filepath.Base(showDirectory(path)))
}

func parseTMDBShowIDFromPath(path string) int {
	match := tmdbIDPattern.FindStringSubmatch(filepath.Base(path))
	if len(match) == 2 {
		id, err := strconv.Atoi(match[1])
		if err == nil && id > 0 {
			return id
		}
	}
	return 0
}

func parseDirectoryYearFromPath(path string) string {
	match := directoryYearPattern.FindStringSubmatch(filepath.Base(path))
	if len(match) > 0 {
		return strings.Trim(match[0], " []{}()")
	}
	return ""
}

func showDirectory(path string) string {
	dir := filepath.Dir(path)
	if seasonDirPattern.MatchString(filepath.Base(dir)) {
		return filepath.Dir(dir)
	}
	return dir
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
		"show":         item.Show,
		"showOriginal": item.ShowOriginal,
		"releaseGroup": item.ReleaseGroup,
		"tmid":         positiveIntString(item.TMDBShowID),
		"tmdbShowId":   positiveIntString(item.TMDBShowID),
		"season":       strconv.Itoa(item.Season),
		"episode":      strconv.Itoa(item.Episode),
		"title":        item.Title,
		"year":         item.Year,
	}
	return placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := placeholderPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return ""
		}
		key := parts[1]
		format := ""
		if len(parts) >= 3 {
			format = strings.TrimSpace(parts[2])
		}
		value := localizedPlaceholderValue(key, format, item, values)
		if len(parts) >= 3 && strings.Trim(parts[2], "0") == "" && strings.TrimSpace(parts[2]) != "" {
			if number, err := strconv.Atoi(value); err == nil {
				return leftPad(strconv.Itoa(number), len(parts[2]))
			}
		}
		return value
	})
}

func localizedPlaceholderValue(key string, format string, item PreviewItem, values map[string]string) string {
	if format != "" && strings.Trim(format, "0") != "" {
		switch key {
		case "show":
			if item.showByLanguage != nil {
				return firstNonEmpty(item.showByLanguage[languageKey(format)], values[key])
			}
		case "title":
			if item.titleByLanguage != nil {
				return firstNonEmpty(item.titleByLanguage[languageKey(format)], values[key])
			}
		}
	}
	return values[key]
}

func applyLocalizedTemplateValues(ctx context.Context, client *tmdb.Client, template string, item *PreviewItem) {
	if client == nil || item.TMDBShowID <= 0 {
		return
	}
	languages := templateLanguages(template)
	if len(languages) == 0 {
		return
	}
	if item.showByLanguage == nil {
		item.showByLanguage = map[string]string{}
	}
	if item.titleByLanguage == nil {
		item.titleByLanguage = map[string]string{}
	}
	for _, language := range languages {
		key := languageKey(language)
		if item.showByLanguage[key] != "" && item.titleByLanguage[key] != "" {
			continue
		}
		localized, err := client.GetLocalizedEpisodeText(ctx, item.TMDBShowID, item.Season, item.Episode, language)
		if err != nil {
			continue
		}
		if localized.ShowName != "" {
			item.showByLanguage[key] = localized.ShowName
		}
		if localized.Title != "" {
			item.titleByLanguage[key] = localized.Title
		}
	}
}

func templateLanguages(template string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, match := range placeholderPattern.FindAllStringSubmatch(template, -1) {
		if len(match) < 3 {
			continue
		}
		name := match[1]
		language := strings.TrimSpace(match[2])
		if language == "" || strings.Trim(language, "0") == "" {
			continue
		}
		if name != "show" && name != "title" {
			continue
		}
		key := languageKey(language)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, language)
	}
	return result
}

func languageKey(language string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(language), "_", "-"))
}

func positiveIntString(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
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

func cleanTMDBQuery(value string) string {
	value = strings.TrimSpace(value)
	value = leadingReleaseGroupPattern.ReplaceAllString(value, "")
	for {
		cleaned := trailingMediaTagPattern.ReplaceAllString(value, "")
		if cleaned == value {
			break
		}
		value = strings.TrimSpace(cleaned)
	}
	value = tmdbIDPattern.ReplaceAllString(value, "")
	value = directoryYearPattern.ReplaceAllString(value, "")
	value = strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ").Replace(value)
	return cleanQuery(value)
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
