package pipeline

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
	"regexp"
	"strconv"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/fanart"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/tmdb"
)

var episodePattern = regexp.MustCompile(`(?i)s(\d{1,2})e(\d{1,4})\b`)
var seasonDirPattern = regexp.MustCompile(`(?i)^(season\s*\d{1,2}|s\d{1,2}|第\s*\d{1,2}\s*季)$`)

type episodeInfo struct {
	Season  int
	Episode int
	Title   string
	Show    string
}

type NFOResult struct {
	Path          string
	ThumbPath     string
	ShowNFOPath   string
	SeasonNFOPath string
	Images        []ImageArtifact
	TMDBStatus    string
	TMDBDetail    string
	TMDBShowName  string
	TMDBEpisode   string
}

type SeriesResult struct {
	ShowNFOPath   string
	SeasonNFOPath string
}

type ImageResult struct {
	Images []ImageArtifact
}

type ImageArtifact struct {
	Type   string
	Path   string
	Status string
	Detail string
}

type episodeNFO struct {
	XMLName   xml.Name      `xml:"episodedetails"`
	Title     string        `xml:"title"`
	ShowTitle string        `xml:"showtitle,omitempty"`
	SortTitle string        `xml:"sorttitle"`
	Runtime   int           `xml:"runtime,omitempty"`
	Season    int           `xml:"season"`
	Episode   int           `xml:"episode"`
	Plot      string        `xml:"plot,omitempty"`
	Outline   string        `xml:"outline,omitempty"`
	Premiered string        `xml:"premiered,omitempty"`
	Aired     string        `xml:"aired,omitempty"`
	Rating    string        `xml:"rating,omitempty"`
	Thumb     string        `xml:"thumb,omitempty"`
	UniqueID  []nfoUniqueID `xml:"uniqueid,omitempty"`
	Actor     []nfoActor    `xml:"actor,omitempty"`
	Director  []string      `xml:"director,omitempty"`
	Writer    []string      `xml:"credits,omitempty"`
	LockData  bool          `xml:"lockdata"`
	FileInfo  *nfoFileInfo  `xml:"fileinfo,omitempty"`
}

type nfoUniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

type nfoActor struct {
	Name      string `xml:"name"`
	Role      string `xml:"role,omitempty"`
	Order     int    `xml:"order,omitempty"`
	Thumb     string `xml:"thumb,omitempty"`
	ProfileID string `xml:"profileid,omitempty"`
}

type tvshowNFO struct {
	XMLName       xml.Name      `xml:"tvshow"`
	Title         string        `xml:"title,omitempty"`
	OriginalTitle string        `xml:"originaltitle,omitempty"`
	Plot          string        `xml:"plot,omitempty"`
	Outline       string        `xml:"outline,omitempty"`
	Premiered     string        `xml:"premiered,omitempty"`
	Status        string        `xml:"status,omitempty"`
	Rating        string        `xml:"rating,omitempty"`
	Genre         []string      `xml:"genre,omitempty"`
	UniqueID      []nfoUniqueID `xml:"uniqueid,omitempty"`
}

type seasonNFO struct {
	XMLName      xml.Name      `xml:"season"`
	Title        string        `xml:"title,omitempty"`
	Season       int           `xml:"seasonnumber,omitempty"`
	EpisodeCount int           `xml:"episodeguide>episodecount,omitempty"`
	Plot         string        `xml:"plot,omitempty"`
	Outline      string        `xml:"outline,omitempty"`
	Premiered    string        `xml:"premiered,omitempty"`
	Aired        string        `xml:"aired,omitempty"`
	UniqueID     []nfoUniqueID `xml:"uniqueid,omitempty"`
}

type nfoFileInfo struct {
	StreamDetails streamDetails `xml:"streamdetails"`
}

type streamDetails struct {
	Video    *videoDetails     `xml:"video,omitempty"`
	Audio    []audioDetails    `xml:"audio,omitempty"`
	Subtitle []subtitleDetails `xml:"subtitle,omitempty"`
}

type videoDetails struct {
	Codec             string `xml:"codec,omitempty"`
	Bitrate           int64  `xml:"bitrate,omitempty"`
	Width             int    `xml:"width,omitempty"`
	Height            int    `xml:"height,omitempty"`
	Aspect            string `xml:"aspect,omitempty"`
	AspectRatio       string `xml:"aspectratio,omitempty"`
	FrameRate         string `xml:"framerate,omitempty"`
	ScanType          string `xml:"scantype,omitempty"`
	Default           bool   `xml:"default,omitempty"`
	Forced            bool   `xml:"forced,omitempty"`
	DurationInSeconds int    `xml:"durationinseconds,omitempty"`
	Duration          int    `xml:"duration,omitempty"`
}

type audioDetails struct {
	Codec        string `xml:"codec,omitempty"`
	Bitrate      int64  `xml:"bitrate,omitempty"`
	Language     string `xml:"language,omitempty"`
	Channels     int    `xml:"channels,omitempty"`
	SamplingRate int    `xml:"samplingrate,omitempty"`
	Default      bool   `xml:"default,omitempty"`
	Forced       bool   `xml:"forced,omitempty"`
}

type subtitleDetails struct {
	Codec    string `xml:"codec,omitempty"`
	Language string `xml:"language,omitempty"`
	Default  bool   `xml:"default,omitempty"`
	Forced   bool   `xml:"forced,omitempty"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeNFOData struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

func GenerateNFO(ctx context.Context, cfg config.Config, media store.MediaFile) (NFOResult, error) {
	if !cfg.Processing.EnableNFO {
		return NFOResult{}, nil
	}

	episode, ok := parseEpisodeInfo(media.Path)
	if !ok {
		return NFOResult{}, nil
	}

	outputPath := strings.TrimSuffix(media.Path, filepath.Ext(media.Path)) + ".nfo"
	if !cfg.Processing.OverwriteExisting {
		if _, err := os.Stat(outputPath); err == nil {
			result := NFOResult{Path: outputPath, TMDBStatus: "skipped", TMDBDetail: "nfo already exists"}
			return result, nil
		}
	}

	streamInfo, runtime, err := buildStreamDetails(ctx, cfg, media.Path)
	if err != nil {
		return NFOResult{}, err
	}

	doc := episodeNFO{
		ShowTitle: episode.Show,
		Title:     episode.Title,
		SortTitle: episode.Title,
		Runtime:   runtime,
		Season:    episode.Season,
		Episode:   episode.Episode,
		LockData:  false,
		FileInfo:  &nfoFileInfo{StreamDetails: streamInfo},
	}

	result := NFOResult{Path: outputPath}
	applyTMDBEpisode(ctx, cfg, episode, &doc, &result)

	if err := writeXMLFile(outputPath, doc); err != nil {
		return NFOResult{}, err
	}
	return result, nil
}

func GenerateSeriesNFO(ctx context.Context, cfg config.Config, media store.MediaFile) (SeriesResult, error) {
	if !cfg.Processing.EnableNFO {
		return SeriesResult{}, nil
	}
	episode, ok := parseEpisodeInfo(media.Path)
	if !ok {
		return SeriesResult{}, nil
	}
	result := NFOResult{Path: strings.TrimSuffix(media.Path, filepath.Ext(media.Path)) + ".nfo"}
	applyTMDBShowAndSeason(ctx, cfg, episode, &result)
	return SeriesResult{ShowNFOPath: result.ShowNFOPath, SeasonNFOPath: result.SeasonNFOPath}, nil
}

func GenerateSeriesImages(ctx context.Context, cfg config.Config, media store.MediaFile) (ImageResult, error) {
	episode, ok := parseEpisodeInfo(media.Path)
	if !ok {
		return ImageResult{}, nil
	}
	result := NFOResult{Path: strings.TrimSuffix(media.Path, filepath.Ext(media.Path)) + ".nfo"}
	applyTMDBShowAndSeasonImages(ctx, cfg, episode, &result)
	return ImageResult{Images: result.Images}, nil
}

func parseEpisodeInfo(path string) (episodeInfo, bool) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	match := episodePattern.FindStringSubmatch(name)
	if len(match) != 3 {
		return episodeInfo{}, false
	}
	season, err := strconv.Atoi(match[1])
	if err != nil {
		return episodeInfo{}, false
	}
	episode, err := strconv.Atoi(match[2])
	if err != nil {
		return episodeInfo{}, false
	}
	return episodeInfo{Season: season, Episode: episode, Title: name, Show: parseShowName(path, name, match[0])}, true
}

func parseShowName(path string, fileTitle string, episodeToken string) string {
	show := strings.TrimSpace(fileTitle)
	if episodeToken != "" {
		index := strings.Index(strings.ToLower(show), strings.ToLower(episodeToken))
		if index > 0 {
			show = show[:index]
		}
	}
	show = cleanTMDBQuery(show)
	if show != "" {
		return show
	}
	return cleanTMDBQuery(filepath.Base(filepath.Dir(path)))
}

func cleanTMDBQuery(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, " .-_[]()")
	value = strings.ReplaceAll(value, ".", " ")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	return strings.Join(strings.Fields(value), " ")
}

func applyTMDBEpisode(ctx context.Context, cfg config.Config, episode episodeInfo, doc *episodeNFO, result *NFOResult) {
	client, err := tmdb.NewClient(cfg.Scraping)
	if err != nil {
		result.TMDBStatus = "disabled"
		result.TMDBDetail = err.Error()
		return
	}
	detail, err := client.FindEpisode(ctx, episode.Show, episode.Season, episode.Episode)
	if err != nil {
		result.TMDBStatus = "failed"
		result.TMDBDetail = err.Error()
		return
	}

	result.TMDBStatus = "matched"
	result.TMDBShowName = detail.ShowName
	result.TMDBEpisode = detail.Title

	if detail.Title != "" {
		doc.Title = detail.Title
		doc.SortTitle = detail.Title
	}
	if detail.ShowName != "" {
		doc.ShowTitle = detail.ShowName
	}
	doc.Plot = detail.Overview
	doc.Outline = detail.Overview
	doc.Premiered = detail.AirDate
	doc.Aired = detail.AirDate
	if detail.VoteAverage > 0 {
		doc.Rating = fmt.Sprintf("%.1f", detail.VoteAverage)
	}
	if detail.ShowID > 0 {
		doc.UniqueID = append(doc.UniqueID, nfoUniqueID{Type: "tmdb", Default: true, Value: strconv.Itoa(detail.ShowID)})
	}
	if detail.EpisodeID > 0 {
		doc.UniqueID = append(doc.UniqueID, nfoUniqueID{Type: "tmdb_episode", Value: strconv.Itoa(detail.EpisodeID)})
	}
	if detail.StillPath != "" {
		thumbPath, err := ensureEpisodeThumb(ctx, cfg, result.Path, client.ImageURL(detail.StillPath))
		if err == nil && thumbPath != "" {
			result.ThumbPath = thumbPath
			doc.Thumb = filepath.Base(thumbPath)
		} else if err != nil && result.TMDBDetail == "" {
			result.TMDBDetail = "thumb download failed: " + err.Error()
		}
	}
	for _, actor := range detail.Actors {
		doc.Actor = append(doc.Actor, nfoActor{
			Name:  actor.Name,
			Role:  actor.Role,
			Order: actor.Order,
			Thumb: client.ImageURL(actor.ProfilePath),
		})
	}
	for _, crew := range detail.Crew {
		switch strings.ToLower(crew.Job) {
		case "director":
			doc.Director = appendUniqueString(doc.Director, crew.Name)
		case "writer", "screenplay", "teleplay", "story":
			doc.Writer = appendUniqueString(doc.Writer, crew.Name)
		}
	}
}

func applyTMDBShowAndSeason(ctx context.Context, cfg config.Config, episode episodeInfo, result *NFOResult) {
	client, err := tmdb.NewClient(cfg.Scraping)
	if err != nil {
		return
	}
	show, season, err := client.FindShowAndSeason(ctx, episode.Show, episode.Season)
	if err != nil {
		if result.TMDBDetail == "" {
			result.TMDBDetail = "show/season failed: " + err.Error()
		}
		return
	}

	showPath := filepath.Join(showNFOBaseDir(result.Path), "tvshow.nfo")
	seasonPath := filepath.Join(filepath.Dir(result.Path), "season.nfo")
	if err := upsertTVShowNFO(showPath, cfg, show); err == nil {
		result.ShowNFOPath = showPath
	} else if result.TMDBDetail == "" {
		result.TMDBDetail = "tvshow nfo failed: " + err.Error()
	}
	if err := upsertSeasonNFO(seasonPath, cfg, season); err == nil {
		result.SeasonNFOPath = seasonPath
	} else if result.TMDBDetail == "" {
		result.TMDBDetail = "season nfo failed: " + err.Error()
	}
}

func applyTMDBShowAndSeasonImages(ctx context.Context, cfg config.Config, episode episodeInfo, result *NFOResult) {
	if !cfg.Processing.EnableImageTakeover || !imageSourceEnabled(cfg, "tmdb") {
		return
	}
	client, err := tmdb.NewClient(cfg.Scraping)
	if err != nil {
		return
	}
	show, season, err := client.FindShowAndSeasonImages(ctx, episode.Show, episode.Season, cfg.Scraping.PreferOriginalLanguagePoster)
	if err != nil {
		if result.TMDBDetail == "" {
			result.TMDBDetail = "show/season images failed: " + err.Error()
		}
		return
	}

	showDir := showNFOBaseDir(result.Path)
	seasonDir := filepath.Dir(result.Path)
	fanartImages := fanart.TVImages{}
	fanartDetail := "fanart not enabled"
	if imageSourceEnabled(cfg, "fanart") && show.TVDBID > 0 {
		if client, err := fanart.NewClient(cfg.Scraping); err == nil {
			if images, err := client.GetTVImages(ctx, show.TVDBID, episode.Season, imageLanguages(cfg)); err == nil {
				fanartImages = images
				fanartDetail = "fanart returned no matching image"
			} else {
				fanartDetail = "fanart request failed: " + err.Error()
			}
		} else {
			fanartDetail = "fanart disabled: " + err.Error()
		}
	} else if imageSourceEnabled(cfg, "fanart") && show.TVDBID <= 0 {
		fanartDetail = "fanart requires tvdb id"
	}
	downloads := []ImageArtifact{
		{Type: "poster", Path: filepath.Join(showDir, "poster.jpg")},
		{Type: "fanart", Path: filepath.Join(showDir, "fanart.jpg")},
		{Type: "clearlogo", Path: filepath.Join(showDir, "clearlogo.png")},
		{Type: "clearart", Path: filepath.Join(showDir, "clearart.png")},
		{Type: "season-poster", Path: seasonPosterPath(showDir, seasonDir, episode.Season)},
	}
	urls := []string{
		chooseImageSource(cfg, map[string]string{"tmdb": client.ImageURL(show.PosterPath), "fanart": fanartImages.Poster}),
		chooseImageSource(cfg, map[string]string{"tmdb": client.ImageURL(show.BackdropPath), "fanart": fanartImages.Fanart}),
		chooseImageSource(cfg, map[string]string{"tmdb": client.ImageURL(show.LogoPath), "fanart": fanartImages.ClearLogo}),
		chooseImageSource(cfg, map[string]string{"fanart": fanartImages.ClearArt}),
		chooseImageSource(cfg, map[string]string{"tmdb": client.ImageURL(season.PosterPath), "fanart": fanartImages.SeasonPoster}),
	}

	for index, item := range downloads {
		if strings.TrimSpace(urls[index]) == "" {
			result.Images = append(result.Images, ImageArtifact{Type: item.Type, Path: item.Path, Status: "unavailable", Detail: imageUnavailableDetail(item.Type, fanartDetail)})
			continue
		}
		path, status, err := ensureImageFile(ctx, cfg, item.Path, urls[index])
		if err != nil {
			if result.TMDBDetail == "" {
				result.TMDBDetail = item.Type + " image failed: " + err.Error()
			}
			continue
		}
		if path != "" {
			result.Images = append(result.Images, ImageArtifact{Type: item.Type, Path: path, Status: status})
		}
	}
}

func imageUnavailableDetail(imageType string, fanartDetail string) string {
	if imageType == "clearart" {
		return fanartDetail
	}
	return "no image candidate from configured sources"
}

func chooseImageSource(cfg config.Config, candidates map[string]string) string {
	for _, source := range imageSourceOrder(cfg) {
		if value := strings.TrimSpace(candidates[strings.ToLower(source)]); value != "" {
			return value
		}
	}
	return ""
}

func imageSourceOrder(cfg config.Config) []string {
	if len(cfg.Scraping.ImageSources) == 0 {
		return []string{"tmdb", "tvdb", "fanart"}
	}
	return cfg.Scraping.ImageSources
}

func imageLanguages(cfg config.Config) []string {
	languages := []string{cfg.Scraping.Language}
	languages = append(languages, cfg.Scraping.FallbackLanguages...)
	return languages
}

func imageSourceEnabled(cfg config.Config, source string) bool {
	if len(cfg.Scraping.ImageSources) == 0 {
		return strings.EqualFold(source, "tmdb")
	}
	for _, item := range cfg.Scraping.ImageSources {
		if strings.EqualFold(strings.TrimSpace(item), source) {
			return true
		}
	}
	return false
}

func seasonPosterPath(showDir string, seasonDir string, season int) string {
	if filepath.Clean(showDir) == filepath.Clean(seasonDir) {
		return filepath.Join(showDir, fmt.Sprintf("season%02d-poster.jpg", season))
	}
	return filepath.Join(seasonDir, "poster.jpg")
}

func showNFOBaseDir(episodeNFOPath string) string {
	dir := filepath.Dir(episodeNFOPath)
	if seasonDirPattern.MatchString(filepath.Base(dir)) {
		return filepath.Dir(dir)
	}
	return dir
}

func upsertTVShowNFO(path string, cfg config.Config, show tmdb.Show) error {
	doc := tvshowNFO{}
	if data, err := os.ReadFile(path); err == nil {
		_ = xml.Unmarshal(data, &doc)
	}
	incoming := tvshowNFO{
		Title:         show.Name,
		OriginalTitle: show.Name,
		Plot:          show.Overview,
		Outline:       show.Overview,
		Premiered:     show.FirstAirDate,
		Status:        show.Status,
		Genre:         show.Genres,
	}
	if show.VoteAverage > 0 {
		incoming.Rating = fmt.Sprintf("%.1f", show.VoteAverage)
	}
	if show.ID > 0 {
		incoming.UniqueID = []nfoUniqueID{{Type: "tmdb", Default: true, Value: strconv.Itoa(show.ID)}}
	}

	doc.XMLName = xml.Name{Local: "tvshow"}
	mergeText(&doc.Title, incoming.Title, cfg.Processing.OverwriteExisting)
	mergeText(&doc.OriginalTitle, incoming.OriginalTitle, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Plot, incoming.Plot, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Outline, incoming.Outline, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Premiered, incoming.Premiered, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Status, incoming.Status, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Rating, incoming.Rating, cfg.Processing.OverwriteExisting)
	if cfg.Processing.OverwriteExisting || len(doc.Genre) == 0 {
		doc.Genre = incoming.Genre
	}
	doc.UniqueID = mergeUniqueIDs(doc.UniqueID, incoming.UniqueID, cfg.Processing.OverwriteExisting)
	return writeXMLFile(path, doc)
}

func upsertSeasonNFO(path string, cfg config.Config, season tmdb.Season) error {
	doc := seasonNFO{}
	if data, err := os.ReadFile(path); err == nil {
		_ = xml.Unmarshal(data, &doc)
	}
	incoming := seasonNFO{
		Title:        season.Name,
		Season:       season.SeasonNumber,
		EpisodeCount: season.EpisodeCount,
		Plot:         season.Overview,
		Outline:      season.Overview,
		Premiered:    season.AirDate,
		Aired:        season.AirDate,
	}
	if season.ID > 0 {
		incoming.UniqueID = []nfoUniqueID{{Type: "tmdb", Default: true, Value: strconv.Itoa(season.ID)}}
	}

	doc.XMLName = xml.Name{Local: "season"}
	mergeText(&doc.Title, incoming.Title, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Plot, incoming.Plot, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Outline, incoming.Outline, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Premiered, incoming.Premiered, cfg.Processing.OverwriteExisting)
	mergeText(&doc.Aired, incoming.Aired, cfg.Processing.OverwriteExisting)
	if cfg.Processing.OverwriteExisting || doc.Season == 0 {
		doc.Season = incoming.Season
	}
	if cfg.Processing.OverwriteExisting || doc.EpisodeCount == 0 {
		doc.EpisodeCount = incoming.EpisodeCount
	}
	doc.UniqueID = mergeUniqueIDs(doc.UniqueID, incoming.UniqueID, cfg.Processing.OverwriteExisting)
	return writeXMLFile(path, doc)
}

func mergeText(target *string, value string, overwrite bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if overwrite || strings.TrimSpace(*target) == "" {
		*target = value
	}
}

func mergeUniqueIDs(existing []nfoUniqueID, incoming []nfoUniqueID, overwrite bool) []nfoUniqueID {
	if overwrite || len(existing) == 0 {
		return incoming
	}
	result := existing
	for _, next := range incoming {
		found := false
		for _, current := range result {
			if strings.EqualFold(current.Type, next.Type) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, next)
		}
	}
	return result
}

func writeXMLFile(path string, doc any) error {
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	content := append([]byte(xml.Header), data...)
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func buildStreamDetails(ctx context.Context, cfg config.Config, mediaPath string) (streamDetails, int, error) {
	if strings.TrimSpace(cfg.Tools.FFprobe) == "" {
		return streamDetails{}, 0, errors.New("ffprobe is not configured")
	}
	output, err := runCommand(ctx, cfg.Tools.FFprobe, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", mediaPath)
	if err != nil {
		return streamDetails{}, 0, err
	}

	var parsed ffprobeNFOData
	if err := json.Unmarshal(output, &parsed); err != nil {
		return streamDetails{}, 0, err
	}

	result := streamDetails{}
	runtime := parseRuntimeMinutes(parsed.Format.Duration)
	durationSeconds := parseDurationSeconds(parsed.Format.Duration)

	for _, stream := range parsed.Streams {
		switch stream.CodecType {
		case "video":
			if result.Video == nil {
				aspect := defaultAspect(stream.DisplayAspectRatio, stream.SampleAspectRatio)
				result.Video = &videoDetails{
					Codec:             stream.CodecName,
					Bitrate:           parseInt64(stream.BitRate),
					Width:             stream.Width,
					Height:            stream.Height,
					Aspect:            aspect,
					AspectRatio:       aspect,
					FrameRate:         chooseFrameRate(stream.AvgFrameRate, stream.RFrameRate),
					ScanType:          "progressive",
					Default:           stream.Disposition.Default == 1,
					Forced:            stream.Disposition.Forced == 1,
					DurationInSeconds: durationSeconds,
					Duration:          runtime,
				}
			}
		case "audio":
			result.Audio = append(result.Audio, audioDetails{
				Codec:        stream.CodecName,
				Bitrate:      parseInt64(stream.BitRate),
				Language:     normalizeLanguage(stream.Tags["language"]),
				Channels:     stream.Channels,
				SamplingRate: stream.SampleRate,
				Default:      stream.Disposition.Default == 1,
				Forced:       stream.Disposition.Forced == 1,
			})
		case "subtitle":
			result.Subtitle = append(result.Subtitle, subtitleDetails{
				Codec:    stream.CodecName,
				Language: normalizeLanguage(stream.Tags["language"]),
				Default:  stream.Disposition.Default == 1,
				Forced:   stream.Disposition.Forced == 1,
			})
		}
	}

	return result, runtime, nil
}

func ensureEpisodeThumb(ctx context.Context, cfg config.Config, nfoPath string, imageURL string) (string, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return "", nil
	}
	thumbPath := strings.TrimSuffix(nfoPath, filepath.Ext(nfoPath)) + "-thumb.jpg"
	path, _, err := ensureImageFile(ctx, cfg, thumbPath, imageURL)
	return path, err
}

func ensureImageFile(ctx context.Context, cfg config.Config, outputPath string, imageURL string) (string, string, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return "", "", nil
	}
	if !cfg.Processing.OverwriteExisting {
		if _, err := os.Stat(outputPath); err == nil {
			return outputPath, "skipped", nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", "", err
	}
	client, err := httpClientForScraping(cfg)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("image request failed: %s", resp.Status)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", "", err
	}
	return outputPath, "generated", nil
}

func httpClientForScraping(cfg config.Config) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(cfg.Scraping.Proxy) != "" {
		proxyURL, err := url.Parse(cfg.Scraping.Proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

func parseRuntimeMinutes(value string) int {
	seconds := parseDurationSeconds(value)
	if seconds <= 0 {
		return 0
	}
	return seconds / 60
}

func parseDurationSeconds(value string) int {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int(floatValue)
}

func parseInt64(value string) int64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func chooseFrameRate(avg string, real string) string {
	if avg != "" && avg != "0/0" {
		return simplifyRate(avg)
	}
	return simplifyRate(real)
}

func simplifyRate(value string) string {
	if !strings.Contains(value, "/") {
		return value
	}
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 {
		return value
	}
	numerator, err1 := strconv.ParseFloat(parts[0], 64)
	denominator, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || denominator == 0 {
		return value
	}
	return fmt.Sprintf("%.3f", numerator/denominator)
}

func defaultAspect(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return ""
}
