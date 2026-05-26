package metadataaudit

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/episodeparse"
	"NyaMediaMetadataTool/internal/tmdb"
)

type Options struct {
	Root         string
	Config       config.Config
	TMDBShowID   int
	EmbyItemURL  string
	EmbyURL      string
	EmbyAPIKey   string
	EmbySeriesID string
}

type Report struct {
	Root            string            `json:"root"`
	ShowTitle       string            `json:"showTitle,omitempty"`
	TMDBShowID      int               `json:"tmdbShowId,omitempty"`
	LocalShow       LocalShow         `json:"localShow"`
	LocalSeasons    []LocalSeason     `json:"localSeasons"`
	LocalEpisodes   []LocalEpisode    `json:"localEpisodes"`
	SeasonReports   []SeasonReport    `json:"seasonReports"`
	ArtifactIssues  []ComparisonIssue `json:"artifactIssues,omitempty"`
	EmbyComparisons []ComparisonIssue `json:"embyComparisons,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
}

type LocalShow struct {
	Title       string            `json:"title,omitempty"`
	Plot        string            `json:"plot,omitempty"`
	HasImage    bool              `json:"hasImage"`
	ProviderIDs map[string]string `json:"providerIds,omitempty"`
}

type LocalSeason struct {
	Season       int               `json:"season"`
	Title        string            `json:"title,omitempty"`
	Plot         string            `json:"plot,omitempty"`
	EpisodeCount int               `json:"episodeCount,omitempty"`
	HasImage     bool              `json:"hasImage"`
	ProviderIDs  map[string]string `json:"providerIds,omitempty"`
}

type LocalEpisode struct {
	Season      int               `json:"season"`
	Episode     int               `json:"episode"`
	Path        string            `json:"path"`
	NFOPath     string            `json:"nfoPath,omitempty"`
	Title       string            `json:"title,omitempty"`
	Plot        string            `json:"plot,omitempty"`
	Thumb       string            `json:"thumb,omitempty"`
	HasImage    bool              `json:"hasImage"`
	ProviderIDs map[string]string `json:"providerIds,omitempty"`
}

type SeasonReport struct {
	Season           int    `json:"season"`
	ExpectedCount    int    `json:"expectedCount,omitempty"`
	ExpectedSource   string `json:"expectedSource,omitempty"`
	ExpectedEpisodes []int  `json:"expectedEpisodes,omitempty"`
	ExistingCount    int    `json:"existingCount"`
	ExistingEpisodes []int  `json:"existingEpisodes"`
	MissingEpisodes  []int  `json:"missingEpisodes,omitempty"`
	Note             string `json:"note,omitempty"`
}

type ComparisonIssue struct {
	Severity string `json:"severity"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	Field    string `json:"field"`
	Local    string `json:"local,omitempty"`
	Emby     string `json:"emby,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type tvshowNFO struct {
	Title    string        `xml:"title"`
	Plot     string        `xml:"plot"`
	Outline  string        `xml:"outline"`
	UniqueID []nfoUniqueID `xml:"uniqueid"`
	TMDBID   string        `xml:"tmdbid"`
}

type seasonNFO struct {
	Title        string        `xml:"title"`
	Season       int           `xml:"seasonnumber"`
	Plot         string        `xml:"plot"`
	Outline      string        `xml:"outline"`
	EpisodeCount int           `xml:"episodeguide>episodecount"`
	UniqueID     []nfoUniqueID `xml:"uniqueid"`
	TMDBID       string        `xml:"tmdbid"`
}

type episodeNFO struct {
	Title    string        `xml:"title"`
	Season   int           `xml:"season"`
	Episode  int           `xml:"episode"`
	Plot     string        `xml:"plot"`
	Outline  string        `xml:"outline"`
	Thumb    string        `xml:"thumb"`
	UniqueID []nfoUniqueID `xml:"uniqueid"`
	TMDBID   string        `xml:"tmdbid"`
}

type nfoUniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

func Run(ctx context.Context, opts Options) (Report, error) {
	root, err := filepath.Abs(strings.TrimSpace(opts.Root))
	if err != nil {
		return Report{}, err
	}
	if root == "" {
		return Report{}, errors.New("root is required")
	}
	report := Report{Root: root}

	localShow, warnings := readLocalShow(root)
	report.LocalShow = localShow
	report.ShowTitle = localShow.Title
	showID := tmdbID(localShow.ProviderIDs)
	report.TMDBShowID = firstInt(opts.TMDBShowID, showID)
	report.Warnings = append(report.Warnings, warnings...)
	report.LocalSeasons = scanLocalSeasons(root)

	episodes, warnings, err := scanLocalEpisodes(root, opts.Config)
	if err != nil {
		return Report{}, err
	}
	report.LocalEpisodes = episodes
	report.Warnings = append(report.Warnings, warnings...)
	report.SeasonReports = buildSeasonReports(ctx, root, opts.Config, report.TMDBShowID, episodes, &report.Warnings)
	report.ArtifactIssues = checkLocalArtifacts(root, report.LocalSeasons, episodes)

	embyURL := strings.TrimSpace(opts.EmbyURL)
	embySeriesID := strings.TrimSpace(opts.EmbySeriesID)
	if strings.TrimSpace(opts.EmbyItemURL) != "" {
		parsedURL, parsedSeriesID, err := ParseEmbyItemURL(opts.EmbyItemURL)
		if err != nil {
			return Report{}, err
		}
		embyURL = firstNonEmpty(embyURL, parsedURL)
		embySeriesID = firstNonEmpty(embySeriesID, parsedSeriesID)
	}
	if embyURL != "" || strings.TrimSpace(opts.EmbyAPIKey) != "" || embySeriesID != "" {
		if embyURL == "" || strings.TrimSpace(opts.EmbyAPIKey) == "" || embySeriesID == "" {
			return Report{}, errors.New("emby item url/api key or emby-url/api-key/series-id must be provided together")
		}
		embyClient, err := newEmbyClient(ctx, embyURL, opts.EmbyAPIKey)
		if err != nil {
			return Report{}, err
		}
		embyShow, err := embyClient.fetchItem(ctx, embySeriesID)
		if err != nil {
			return Report{}, err
		}
		embySeasons, err := embyClient.fetchSeasons(ctx, embySeriesID)
		if err != nil {
			return Report{}, err
		}
		embyEpisodes, err := embyClient.fetchEpisodes(ctx, embySeriesID)
		if err != nil {
			return Report{}, err
		}
		report.EmbyComparisons = compareEmby(localShow, report.LocalSeasons, episodes, embyShow, embySeasons, embyEpisodes)
	}

	return report, nil
}

func ParseEmbyItemURL(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", errors.New("emby item url is empty")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", errors.New("emby item url must include scheme and host")
	}

	seriesID := strings.TrimSpace(parsed.Query().Get("id"))
	if seriesID == "" && strings.TrimSpace(parsed.Fragment) != "" {
		fragment := strings.TrimPrefix(strings.TrimSpace(parsed.Fragment), "!")
		if index := strings.Index(fragment, "?"); index >= 0 {
			query, err := url.ParseQuery(fragment[index+1:])
			if err == nil {
				seriesID = strings.TrimSpace(query.Get("id"))
			}
		}
	}
	if seriesID == "" {
		return "", "", errors.New("emby item url does not contain item id")
	}

	basePath := parsed.EscapedPath()
	if index := strings.Index(strings.ToLower(basePath), "/web/"); index >= 0 {
		basePath = basePath[:index]
	} else if strings.HasSuffix(strings.ToLower(basePath), "/web") {
		basePath = strings.TrimSuffix(basePath, basePath[len(basePath)-4:])
	}
	if basePath == "/" {
		basePath = ""
	}
	base := parsed.Scheme + "://" + parsed.Host + strings.TrimRight(basePath, "/")
	return base, seriesID, nil
}

func HasIssues(report Report) bool {
	for _, season := range report.SeasonReports {
		if len(season.MissingEpisodes) > 0 {
			return true
		}
	}
	return len(report.EmbyComparisons) > 0 || len(report.Warnings) > 0
}

func WriteText(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "目录: %s\n", report.Root); err != nil {
		return err
	}
	if report.ShowTitle != "" || report.TMDBShowID != 0 {
		if _, err := fmt.Fprintf(w, "剧集: %s", report.ShowTitle); err != nil {
			return err
		}
		if report.TMDBShowID != 0 {
			if _, err := fmt.Fprintf(w, " (TMDB %d)", report.TMDBShowID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "本地单集: %d\n\n", len(report.LocalEpisodes)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "季度缺漏:"); err != nil {
		return err
	}
	if len(report.SeasonReports) == 0 {
		if _, err := fmt.Fprintln(w, "  未发现 Season 1+ 单集"); err != nil {
			return err
		}
	}
	for _, season := range report.SeasonReports {
		expected := "未知"
		if season.ExpectedCount > 0 {
			expected = strconv.Itoa(season.ExpectedCount)
		}
		missing := "无"
		if len(season.MissingEpisodes) > 0 {
			missing = intList(season.MissingEpisodes)
		}
		if _, err := fmt.Fprintf(w, "  S%02d: 已有 %s / 期望 %s", season.Season, intList(season.ExistingEpisodes), expected); err != nil {
			return err
		}
		if season.ExpectedSource != "" {
			if _, err := fmt.Fprintf(w, " (%s)", season.ExpectedSource); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "; 缺失 %s\n", missing); err != nil {
			return err
		}
		if season.Note != "" {
			if _, err := fmt.Fprintf(w, "    说明: %s\n", season.Note); err != nil {
				return err
			}
		}
	}
	if len(report.EmbyComparisons) > 0 {
		if _, err := fmt.Fprintln(w, "\nEmby 对比:"); err != nil {
			return err
		}
		for _, issue := range report.EmbyComparisons {
			where := "剧集"
			if issue.Season > 0 || issue.Episode > 0 {
				where = fmt.Sprintf("S%02dE%02d", issue.Season, issue.Episode)
			}
			if _, err := fmt.Fprintf(w, "  [%s] %s %s: 本地=%q Emby=%q %s\n", issue.Severity, where, issue.Field, issue.Local, issue.Emby, issue.Detail); err != nil {
				return err
			}
		}
	}
	if len(report.Warnings) > 0 {
		if _, err := fmt.Fprintln(w, "\n警告:"); err != nil {
			return err
		}
		for _, warning := range report.Warnings {
			if _, err := fmt.Fprintf(w, "  %s\n", warning); err != nil {
				return err
			}
		}
	}
	return nil
}

func WriteJSON(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func scanLocalEpisodes(root string, cfg config.Config) ([]LocalEpisode, []string, error) {
	exts := extensionSet(cfg.Processing.Extensions)
	var episodes []LocalEpisode
	var warnings []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipIgnoredDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !exts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		episode, warning := localEpisodeFromVideo(path, cfg)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if episode.Season == 0 {
			return nil
		}
		episodes = append(episodes, episode)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Season == episodes[j].Season {
			return episodes[i].Episode < episodes[j].Episode
		}
		return episodes[i].Season < episodes[j].Season
	})
	return episodes, warnings, nil
}

func localEpisodeFromVideo(path string, cfg config.Config) (LocalEpisode, string) {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	episode := LocalEpisode{Path: path, NFOPath: base + ".nfo"}
	if doc, ok := readEpisodeNFO(episode.NFOPath); ok {
		episode.Season = doc.Season
		episode.Episode = doc.Episode
		episode.Title = strings.TrimSpace(doc.Title)
		episode.Plot = firstNonEmpty(doc.Plot, doc.Outline)
		episode.Thumb = strings.TrimSpace(doc.Thumb)
		episode.ProviderIDs = providerIDs(doc.UniqueID, doc.TMDBID)
	}
	if episode.Season == 0 || episode.Episode == 0 {
		if parsed, ok := episodeparse.Parse(filepath.Base(base), cfg.Processing.EpisodePatterns); ok {
			episode.Season = parsed.Season
			episode.Episode = parsed.Episode
		}
	}
	episode.HasImage = episodeHasImage(base, filepath.Dir(path), episode.Thumb)
	if episode.Season == 0 || episode.Episode == 0 {
		return episode, "无法识别季度/集数: " + path
	}
	return episode, ""
}

func buildSeasonReports(ctx context.Context, root string, cfg config.Config, showID int, episodes []LocalEpisode, warnings *[]string) []SeasonReport {
	bySeason := make(map[int]map[int]bool)
	seasonDirs := make(map[int]string)
	for _, episode := range episodes {
		if episode.Season == 0 {
			continue
		}
		if bySeason[episode.Season] == nil {
			bySeason[episode.Season] = map[int]bool{}
			seasonDirs[episode.Season] = filepath.Dir(episode.Path)
		}
		bySeason[episode.Season][episode.Episode] = true
	}

	var tmdbClient *tmdb.Client
	if showID != 0 {
		scraping := cfg.Scraping
		scraping.EnableTMDB = true
		client, err := tmdb.NewClient(scraping)
		if err == nil {
			tmdbClient = client
		} else if warnings != nil {
			*warnings = append(*warnings, "已识别 TMDB ID，但 TMDB 不可用，回退到 season.nfo: "+err.Error())
		}
	}

	seasons := make([]int, 0, len(bySeason))
	for season := range bySeason {
		if season == 0 {
			continue
		}
		seasons = append(seasons, season)
	}
	sort.Ints(seasons)
	reports := make([]SeasonReport, 0, len(seasons))
	for _, season := range seasons {
		existing := setToSortedInts(bySeason[season])
		expected, expectedEpisodes, source := expectedEpisodeInfo(ctx, root, seasonDirs[season], season, showID, tmdbClient, warnings)
		report := SeasonReport{Season: season, ExpectedCount: expected, ExpectedSource: source, ExpectedEpisodes: expectedEpisodes, ExistingCount: len(existing), ExistingEpisodes: existing}
		applyMissingEpisodes(&report, bySeason[season])
		reports = append(reports, report)
	}
	return reports
}

func expectedEpisodeInfo(ctx context.Context, root string, seasonDir string, season int, showID int, client *tmdb.Client, warnings *[]string) (int, []int, string) {
	if client != nil && showID != 0 {
		_, seasonDetail, err := client.FindShowAndSeasonByShowID(ctx, showID, season)
		if err == nil && seasonDetail.EpisodeCount > 0 {
			return seasonDetail.EpisodeCount, seasonDetail.EpisodeNumbers, "tmdb"
		}
		if err != nil && warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf("TMDB S%02d 获取失败: %v", season, err))
		}
	}
	for _, dir := range uniqueStrings([]string{seasonDir, filepath.Join(root, fmt.Sprintf("Season %02d", season)), filepath.Join(root, fmt.Sprintf("Season %d", season))}) {
		if count := readSeasonEpisodeCount(filepath.Join(dir, "season.nfo")); count > 0 {
			return count, nil, "season.nfo"
		}
	}
	return 0, nil, ""
}

func applyMissingEpisodes(report *SeasonReport, existingSet map[int]bool) {
	if len(report.ExpectedEpisodes) > 0 {
		for _, episode := range report.ExpectedEpisodes {
			if !existingSet[episode] {
				report.MissingEpisodes = append(report.MissingEpisodes, episode)
			}
		}
		return
	}
	if report.ExpectedCount <= 0 {
		return
	}
	if report.ExistingCount == report.ExpectedCount {
		report.Note = "只有集数总量，未假设季度内编号必须从 1 开始"
		return
	}
	if maxInt(report.ExistingEpisodes) <= report.ExpectedCount {
		for episode := 1; episode <= report.ExpectedCount; episode++ {
			if !existingSet[episode] {
				report.MissingEpisodes = append(report.MissingEpisodes, episode)
			}
		}
		return
	}
	report.Note = "只有集数总量，现有编号超出总量范围，需提供 TMDB 才能精确判断缺漏"
}

func readLocalShow(root string) (LocalShow, []string) {
	path := filepath.Join(root, "tvshow.nfo")
	show := LocalShow{HasImage: directoryHasImage(root, []string{"poster", "fanart", "backdrop", "clearlogo", "clearart"})}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return show, nil
		}
		return show, []string{"读取 tvshow.nfo 失败: " + err.Error()}
	}
	var doc tvshowNFO
	if err := xml.Unmarshal(data, &doc); err != nil {
		return show, []string{"解析 tvshow.nfo 失败: " + err.Error()}
	}
	show.Title = strings.TrimSpace(doc.Title)
	show.Plot = firstNonEmpty(doc.Plot, doc.Outline)
	show.ProviderIDs = providerIDs(doc.UniqueID, doc.TMDBID)
	return show, nil
}

func scanLocalSeasons(root string) []LocalSeason {
	var seasons []LocalSeason
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipIgnoredDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(entry.Name(), "season.nfo") {
			return nil
		}
		season, ok := readLocalSeason(path)
		if !ok {
			return nil
		}
		seasons = append(seasons, season)
		return nil
	})
	sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season < seasons[j].Season })
	return seasons
}

func readLocalSeason(path string) (LocalSeason, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LocalSeason{}, false
	}
	var doc seasonNFO
	if err := xml.Unmarshal(data, &doc); err != nil {
		return LocalSeason{}, false
	}
	dir := filepath.Dir(path)
	seasonNumber := doc.Season
	if seasonNumber == 0 {
		seasonNumber = numberFromString(filepath.Base(dir))
	}
	return LocalSeason{
		Season:       seasonNumber,
		Title:        strings.TrimSpace(doc.Title),
		Plot:         firstNonEmpty(doc.Plot, doc.Outline),
		EpisodeCount: doc.EpisodeCount,
		HasImage:     directoryHasImage(dir, []string{"poster", "folder", "season", "fanart", "backdrop"}),
		ProviderIDs:  providerIDs(doc.UniqueID, doc.TMDBID),
	}, seasonNumber != 0
}

func readSeasonEpisodeCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var doc seasonNFO
	if err := xml.Unmarshal(data, &doc); err != nil {
		return 0
	}
	return doc.EpisodeCount
}

func readEpisodeNFO(path string) (episodeNFO, bool) {
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

func extensionSet(exts []string) map[string]bool {
	set := make(map[string]bool, len(exts))
	for _, ext := range exts {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		set[ext] = true
	}
	return set
}

func episodeHasImage(base string, dir string, thumb string) bool {
	if strings.TrimSpace(thumb) != "" && fileExists(filepath.Join(dir, filepath.FromSlash(thumb))) {
		return true
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if fileExists(base + "-thumb" + ext) {
			return true
		}
	}
	return false
}

func directoryHasImage(dir string, stems []string) bool {
	for _, stem := range stems {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			if fileExists(filepath.Join(dir, stem+ext)) {
				return true
			}
		}
	}
	return false
}

func checkLocalArtifacts(root string, seasons []LocalSeason, episodes []LocalEpisode) []ComparisonIssue {
	var issues []ComparisonIssue
	add := func(season int, episode int, field string, detail string) {
		issues = append(issues, ComparisonIssue{Severity: "warning", Season: season, Episode: episode, Field: field, Detail: detail})
	}
	for _, name := range []string{"tvshow.nfo"} {
		if !fileExists(filepath.Join(root, name)) {
			add(0, 0, "series."+name, "剧集级产物缺失")
		}
	}
	for _, image := range []string{"poster", "fanart", "backdrop", "clearlogo", "clearart"} {
		if !hasAnyExt(root, image, []string{".jpg", ".jpeg", ".png", ".webp"}) {
			add(0, 0, "series.image."+image, "剧集图片缺失")
		}
	}
	for _, season := range seasons {
		seasonDir := seasonDirectory(root, season.Season, episodes)
		if seasonDir == "" {
			continue
		}
		if !fileExists(filepath.Join(seasonDir, "season.nfo")) {
			add(season.Season, 0, "season.nfo", "季度 NFO 缺失")
		}
		if !directoryHasImage(seasonDir, []string{"poster", "folder", "season"}) {
			add(season.Season, 0, "season.image", "季度图片缺失")
		}
	}
	for _, episode := range episodes {
		base := strings.TrimSuffix(episode.Path, filepath.Ext(episode.Path))
		if !fileExists(base + ".nfo") {
			add(episode.Season, episode.Episode, "episode.nfo", "单集 NFO 缺失")
		}
		if !episodeHasImage(base, filepath.Dir(episode.Path), episode.Thumb) {
			add(episode.Season, episode.Episode, "episode.thumb", "单集图片缺失")
		}
		if !fileExists(base + "-mediainfo.json") {
			add(episode.Season, episode.Episode, "episode.mediainfo", "mediainfo.json 缺失")
		}
		if !hasBIF(base) {
			add(episode.Season, episode.Episode, "episode.bif", "BIF 缺失")
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Season == issues[j].Season {
			if issues[i].Episode == issues[j].Episode {
				return issues[i].Field < issues[j].Field
			}
			return issues[i].Episode < issues[j].Episode
		}
		return issues[i].Season < issues[j].Season
	})
	return issues
}

func seasonDirectory(root string, season int, episodes []LocalEpisode) string {
	for _, episode := range episodes {
		if episode.Season == season {
			return filepath.Dir(episode.Path)
		}
	}
	for _, dir := range []string{filepath.Join(root, fmt.Sprintf("Season %02d", season)), filepath.Join(root, fmt.Sprintf("Season %d", season))} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func hasAnyExt(dir string, stem string, exts []string) bool {
	for _, ext := range exts {
		if fileExists(filepath.Join(dir, stem+ext)) {
			return true
		}
	}
	return false
}

func hasBIF(base string) bool {
	matches, err := filepath.Glob(base + "-*.bif")
	return err == nil && len(matches) > 0
}

func shouldSkipIgnoredDir(path string) bool {
	return fileExists(filepath.Join(path, ".ignore"))
}

func providerIDs(ids []nfoUniqueID, tmdbValue string) map[string]string {
	result := map[string]string{}
	for _, id := range ids {
		typ := strings.ToLower(strings.TrimSpace(id.Type))
		value := strings.TrimSpace(id.Value)
		if typ != "" && value != "" {
			result[typ] = value
		}
	}
	if strings.TrimSpace(tmdbValue) != "" {
		result["tmdb"] = strings.TrimSpace(tmdbValue)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func tmdbID(ids map[string]string) int {
	if ids == nil {
		return 0
	}
	value := strings.TrimSpace(ids["tmdb"])
	if value == "" {
		value = strings.TrimSpace(ids["tmdbid"])
	}
	number, _ := strconv.Atoi(value)
	return number
}

type embyEpisode struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Path              string            `json:"Path"`
	Overview          string            `json:"Overview"`
	IndexNumber       int               `json:"IndexNumber"`
	ParentIndexNumber int               `json:"ParentIndexNumber"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	ImageTags         map[string]string `json:"ImageTags"`
	MediaSources      []embyMediaSource `json:"MediaSources"`
}

type embyMediaSource struct {
	Name string `json:"Name"`
	Path string `json:"Path"`
}

type embyItemsResponse struct {
	Items []embyEpisode `json:"Items"`
}

type embyUser struct {
	ID string `json:"Id"`
}

type embyClient struct {
	baseURL string
	apiKey  string
	userID  string
}

func newEmbyClient(ctx context.Context, baseURL string, apiKey string) (embyClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	var lastErr error
	for _, base := range embyBaseCandidates(baseURL) {
		client := embyClient{baseURL: base, apiKey: apiKey}
		var users []embyUser
		if err := client.get(ctx, "/Users", nil, &users); err != nil {
			lastErr = err
			continue
		}
		for _, user := range users {
			if strings.TrimSpace(user.ID) != "" {
				client.userID = strings.TrimSpace(user.ID)
				return client, nil
			}
		}
		lastErr = errors.New("emby users response is empty")
	}
	if lastErr == nil {
		lastErr = errors.New("emby api base is invalid")
	}
	return embyClient{}, lastErr
}

func embyBaseCandidates(baseURL string) []string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil
	}
	candidates := []string{base}
	if !strings.HasSuffix(strings.ToLower(base), "/emby") {
		candidates = append(candidates, base+"/emby")
	}
	return candidates
}

func (c embyClient) fetchItem(ctx context.Context, itemID string) (embyEpisode, error) {
	fields := url.Values{"Fields": {"Overview,ProviderIds,ImageTags,IndexNumber,ParentIndexNumber,MediaSources"}}
	var parsed embyEpisode
	if err := c.get(ctx, "/Users/"+url.PathEscape(c.userID)+"/Items/"+url.PathEscape(strings.TrimSpace(itemID)), fields, &parsed); err == nil {
		return parsed, nil
	}
	var items embyItemsResponse
	query := url.Values{"Ids": {strings.TrimSpace(itemID)}, "Fields": {"Overview,ProviderIds,ImageTags,IndexNumber,ParentIndexNumber,MediaSources"}}
	if err := c.get(ctx, "/Items", query, &items); err != nil {
		return embyEpisode{}, err
	}
	if len(items.Items) == 0 {
		return embyEpisode{}, errors.New("emby item not found")
	}
	return items.Items[0], nil
}

func (c embyClient) fetchSeasons(ctx context.Context, seriesID string) ([]embyEpisode, error) {
	query := url.Values{"Fields": {"Overview,ProviderIds,ImageTags,IndexNumber"}}
	var parsed embyItemsResponse
	if err := c.get(ctx, "/Shows/"+url.PathEscape(strings.TrimSpace(seriesID))+"/Seasons", query, &parsed); err == nil {
		return parsed.Items, nil
	}
	query = url.Values{"ParentId": {strings.TrimSpace(seriesID)}, "IncludeItemTypes": {"Season"}, "Fields": {"Overview,ProviderIds,ImageTags,IndexNumber"}}
	if err := c.get(ctx, "/Users/"+url.PathEscape(c.userID)+"/Items", query, &parsed); err != nil {
		return nil, err
	}
	return parsed.Items, nil
}

func (c embyClient) fetchEpisodes(ctx context.Context, seriesID string) ([]embyEpisode, error) {
	query := url.Values{"Fields": {"Overview,Path,ProviderIds,ImageTags,ParentIndexNumber,IndexNumber,MediaSources"}}
	var parsed embyItemsResponse
	if err := c.get(ctx, "/Shows/"+url.PathEscape(strings.TrimSpace(seriesID))+"/Episodes", query, &parsed); err == nil {
		return parsed.Items, nil
	}
	query = url.Values{"ParentId": {strings.TrimSpace(seriesID)}, "IncludeItemTypes": {"Episode"}, "Recursive": {"true"}, "Fields": {"Overview,Path,ProviderIds,ImageTags,ParentIndexNumber,IndexNumber,MediaSources"}}
	if err := c.get(ctx, "/Users/"+url.PathEscape(c.userID)+"/Items", query, &parsed); err != nil {
		return nil, err
	}
	return parsed.Items, nil
}

func (c embyClient) get(ctx context.Context, path string, query url.Values, target any) error {
	requestURL, err := embyRequestURL(c.baseURL, path, c.apiKey, query)
	if err != nil {
		return err
	}
	return getEmbyJSON(ctx, requestURL.String(), target)
}

func embyRequestURL(baseURL string, path string, apiKey string, query url.Values) (*url.URL, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	requestURL, err := url.Parse(base + path)
	if err != nil {
		return nil, err
	}
	values := requestURL.Query()
	for key, list := range query {
		for _, value := range list {
			values.Add(key, value)
		}
	}
	values.Set("api_key", strings.TrimSpace(apiKey))
	requestURL.RawQuery = values.Encode()
	return requestURL, nil
}

func getEmbyJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("emby request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func compareEmby(localShow LocalShow, localSeasons []LocalSeason, localEpisodes []LocalEpisode, embyShow embyEpisode, embySeasons []embyEpisode, embyEpisodes []embyEpisode) []ComparisonIssue {
	issues := compareShowFields(localShow, embyShow)
	issues = append(issues, compareSeasonList(localSeasons, embySeasons)...)

	localByKey := make(map[string][]LocalEpisode, len(localEpisodes))
	for _, episode := range localEpisodes {
		key := episodeKey(episode.Season, episode.Episode)
		localByKey[key] = append(localByKey[key], episode)
	}
	embyByKey := make(map[string][]embyEpisode, len(embyEpisodes))
	for _, episode := range embyEpisodes {
		if episode.ParentIndexNumber == 0 {
			continue
		}
		key := episodeKey(episode.ParentIndexNumber, episode.IndexNumber)
		embyByKey[key] = append(embyByKey[key], episode)
	}

	for key, localGroup := range localByKey {
		embyGroup, ok := embyByKey[key]
		if !ok {
			localEpisode := localGroup[0]
			issues = append(issues, ComparisonIssue{Severity: "error", Season: localEpisode.Season, Episode: localEpisode.Episode, Field: "episode", Local: filepath.Base(localEpisode.Path), Detail: "Emby 中缺少该单集"})
			continue
		}
		issues = append(issues, compareEpisodeFields(localGroup[0], embyGroup[0])...)
		issues = append(issues, compareEpisodeSources(localGroup, embyGroup)...)
	}
	for key, embyGroup := range embyByKey {
		if _, ok := localByKey[key]; !ok {
			embyEpisode := embyGroup[0]
			issues = append(issues, ComparisonIssue{Severity: "warning", Season: embyEpisode.ParentIndexNumber, Episode: embyEpisode.IndexNumber, Field: "episode", Emby: embyEpisode.Name, Detail: "Emby 中存在但本地目录未发现"})
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Season == issues[j].Season {
			if issues[i].Episode == issues[j].Episode {
				return issues[i].Field < issues[j].Field
			}
			return issues[i].Episode < issues[j].Episode
		}
		return issues[i].Season < issues[j].Season
	})
	return issues
}

func compareShowFields(local LocalShow, emby embyEpisode) []ComparisonIssue {
	var issues []ComparisonIssue
	add := func(field string, localValue string, embyValue string, detail string) {
		issues = append(issues, ComparisonIssue{Severity: "warning", Field: "series." + field, Local: localValue, Emby: embyValue, Detail: detail})
	}
	if local.Title == "" {
		add("title", "", emby.Name, "本地剧集标题缺失")
	} else if emby.Name == "" {
		add("title", local.Title, "", "Emby 剧集标题缺失")
	} else if normalizeText(local.Title) != normalizeText(emby.Name) {
		add("title", local.Title, emby.Name, "剧集标题不一致")
	}
	if local.Plot == "" {
		if emby.Overview != "" {
			add("plot", "", emby.Overview, "本地剧集简介缺失")
		}
	} else if emby.Overview == "" {
		add("plot", local.Plot, "", "Emby 剧集简介缺失")
	} else if normalizeText(local.Plot) != normalizeText(emby.Overview) {
		add("plot", local.Plot, emby.Overview, "剧集简介不一致")
	}
	if localTMDB := local.ProviderIDs["tmdb"]; localTMDB != "" {
		if embyTMDB := embyProviderID(emby.ProviderIDs, "Tmdb"); embyTMDB == "" {
			add("tmdb", localTMDB, "", "Emby 剧集 TMDB ID 缺失")
		} else if localTMDB != embyTMDB {
			add("tmdb", localTMDB, embyTMDB, "剧集 TMDB ID 不一致")
		}
	}
	if local.HasImage != embyHasImage(emby) {
		add("image", strconv.FormatBool(local.HasImage), strconv.FormatBool(embyHasImage(emby)), "剧集图片存在性不一致")
	}
	return issues
}

func compareSeasonList(local []LocalSeason, emby []embyEpisode) []ComparisonIssue {
	localBySeason := make(map[int]LocalSeason, len(local))
	for _, season := range local {
		localBySeason[season.Season] = season
	}
	embyBySeason := make(map[int]embyEpisode, len(emby))
	for _, season := range emby {
		embyBySeason[season.IndexNumber] = season
	}
	var issues []ComparisonIssue
	for seasonNumber, localSeason := range localBySeason {
		embySeason, ok := embyBySeason[seasonNumber]
		if !ok {
			issues = append(issues, ComparisonIssue{Severity: "warning", Season: seasonNumber, Field: "season", Local: localSeason.Title, Detail: "Emby 中缺少该季度"})
			continue
		}
		issues = append(issues, compareSeasonFields(localSeason, embySeason)...)
	}
	for seasonNumber, embySeason := range embyBySeason {
		if seasonNumber == 0 {
			continue
		}
		if _, ok := localBySeason[seasonNumber]; !ok {
			issues = append(issues, ComparisonIssue{Severity: "warning", Season: seasonNumber, Field: "season", Emby: embySeason.Name, Detail: "Emby 中存在但本地未发现 season.nfo"})
		}
	}
	return issues
}

func compareSeasonFields(local LocalSeason, emby embyEpisode) []ComparisonIssue {
	var issues []ComparisonIssue
	add := func(field string, localValue string, embyValue string, detail string) {
		issues = append(issues, ComparisonIssue{Severity: "warning", Season: local.Season, Field: "season." + field, Local: localValue, Emby: embyValue, Detail: detail})
	}
	if local.Title != "" && emby.Name != "" && normalizeText(local.Title) != normalizeText(emby.Name) {
		add("title", local.Title, emby.Name, "季度标题不一致")
	}
	if local.Plot == "" {
		if emby.Overview != "" {
			add("plot", "", emby.Overview, "本地季度简介缺失")
		}
	} else if emby.Overview == "" {
		add("plot", local.Plot, "", "Emby 季度简介缺失")
	} else if normalizeText(local.Plot) != normalizeText(emby.Overview) {
		add("plot", local.Plot, emby.Overview, "季度简介不一致")
	}
	if local.HasImage != embyHasImage(emby) {
		add("image", strconv.FormatBool(local.HasImage), strconv.FormatBool(embyHasImage(emby)), "季度图片存在性不一致")
	}
	return issues
}

func compareEpisodeFields(local LocalEpisode, emby embyEpisode) []ComparisonIssue {
	var issues []ComparisonIssue
	add := func(field string, localValue string, embyValue string, detail string) {
		issues = append(issues, ComparisonIssue{Severity: "warning", Season: local.Season, Episode: local.Episode, Field: field, Local: localValue, Emby: embyValue, Detail: detail})
	}
	if local.Title == "" {
		add("title", "", emby.Name, "本地单集标题缺失")
	} else if emby.Name == "" {
		add("title", local.Title, "", "Emby 单集标题缺失")
	} else if normalizeText(local.Title) != normalizeText(emby.Name) {
		add("title", local.Title, emby.Name, "标题不一致")
	}
	if local.Plot == "" {
		if emby.Overview != "" {
			add("plot", "", emby.Overview, "本地单集简介缺失")
		}
	} else if emby.Overview == "" {
		add("plot", local.Plot, "", "Emby 单集简介缺失")
	} else if normalizeText(local.Plot) != normalizeText(emby.Overview) {
		add("plot", local.Plot, emby.Overview, "简介不一致")
	}
	if localTMDB := local.ProviderIDs["tmdb"]; localTMDB != "" {
		if embyTMDB := embyProviderID(emby.ProviderIDs, "Tmdb"); embyTMDB != "" && localTMDB != embyTMDB {
			add("tmdb", localTMDB, embyTMDB, "TMDB ID 不一致")
		}
	}
	if local.HasImage != embyHasImage(emby) {
		add("image", strconv.FormatBool(local.HasImage), strconv.FormatBool(embyHasImage(emby)), "单集图片存在性不一致")
	}
	return issues
}

func compareEpisodeSources(local []LocalEpisode, emby []embyEpisode) []ComparisonIssue {
	if len(local) == 0 || len(emby) == 0 {
		return nil
	}
	season := local[0].Season
	episode := local[0].Episode
	localSources := make(map[string]string)
	for _, item := range local {
		if item.Path == "" {
			continue
		}
		localSources[mediaStemKey(item.Path)] = mediaBaseName(item.Path)
	}
	embySources := make(map[string]string)
	for _, item := range emby {
		for _, path := range embyEpisodeSourcePaths(item) {
			embySources[mediaStemKey(path)] = mediaBaseName(path)
		}
	}
	var issues []ComparisonIssue
	for key, localName := range localSources {
		if _, ok := embySources[key]; !ok {
			issues = append(issues, ComparisonIssue{Severity: "warning", Season: season, Episode: episode, Field: "file", Local: localName, Detail: "Emby 中缺少该视频源"})
		}
	}
	for key, embyName := range embySources {
		if _, ok := localSources[key]; !ok {
			issues = append(issues, ComparisonIssue{Severity: "warning", Season: season, Episode: episode, Field: "file", Emby: embyName, Detail: "Emby 中存在但本地目录未发现该视频源"})
		}
	}
	return issues
}

func embyEpisodeSourcePaths(episode embyEpisode) []string {
	seen := map[string]struct{}{}
	var paths []string
	appendPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}
	appendPath(episode.Path)
	for _, source := range episode.MediaSources {
		appendPath(source.Path)
	}
	return paths
}

func sameMediaStem(left string, right string) bool {
	return mediaStemKey(left) == mediaStemKey(right)
}

func mediaStemKey(path string) string {
	name := mediaBaseName(path)
	stem := strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	return strings.ToLower(strings.Join(strings.Fields(stem), " "))
}

func mediaBaseName(path string) string {
	name := filepath.Base(strings.TrimSpace(path))
	if decoded, err := url.PathUnescape(name); err == nil {
		return decoded
	}
	return name
}

func embyProviderID(ids map[string]string, key string) string {
	for name, value := range ids {
		if strings.EqualFold(name, key) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func embyHasImage(episode embyEpisode) bool {
	if len(episode.ImageTags) == 0 {
		return false
	}
	return episode.ImageTags["Primary"] != "" || episode.ImageTags["Thumb"] != ""
}

func episodeKey(season int, episode int) string {
	return strconv.Itoa(season) + "x" + strconv.Itoa(episode)
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func setToSortedInts(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func maxInt(values []int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func numberFromString(value string) int {
	current := 0
	found := false
	for _, char := range value {
		if char >= '0' && char <= '9' {
			current = current*10 + int(char-'0')
			found = true
			continue
		}
		if found {
			return current
		}
	}
	if found {
		return current
	}
	return 0
}

func intList(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}

func firstInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
