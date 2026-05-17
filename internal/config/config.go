package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `json:"server" yaml:"server"`
	Database   DatabaseConfig   `json:"database" yaml:"database"`
	Tools      ToolsConfig      `json:"tools" yaml:"tools"`
	Processing ProcessingConfig `json:"processing" yaml:"processing"`
	Scraping   ScrapingConfig   `json:"scraping" yaml:"scraping"`
	WatchDirs  []WatchDir       `json:"-" yaml:"-"`
}

type ServerConfig struct {
	Addr     string `json:"addr" yaml:"addr"`
	Timezone string `json:"timezone" yaml:"timezone"`
}

type DatabaseConfig struct {
	Path string `json:"path" yaml:"path"`
}

type ToolsConfig struct {
	FFmpeg     string `json:"ffmpeg" yaml:"ffmpeg"`
	FFprobe    string `json:"ffprobe" yaml:"ffprobe"`
	MKVExtract string `json:"mkvextract" yaml:"mkvextract"`
	MediaInfo  string `json:"mediainfo" yaml:"mediainfo"`
}

type ProcessingConfig struct {
	Extensions          []string      `json:"extensions" yaml:"extensions"`
	EpisodePatterns     []string      `json:"episodePatterns" yaml:"episodePatterns"`
	Concurrency         int           `json:"concurrency" yaml:"concurrency"`
	StableDelay         time.Duration `json:"stableDelay" yaml:"stableDelay"`
	StableChecks        int           `json:"stableChecks" yaml:"stableChecks"`
	BIFWidth            int           `json:"bifWidth" yaml:"bifWidth"`
	BIFInterval         int           `json:"bifInterval" yaml:"bifInterval"`
	BIFHWAccel          string        `json:"bifHwAccel" yaml:"bifHwAccel"`
	OverwriteExisting   bool          `json:"overwriteExisting" yaml:"overwriteExisting"`
	EnableSubtitles     bool          `json:"enableSubtitles" yaml:"enableSubtitles"`
	EnableMediaInfo     bool          `json:"enableMediaInfo" yaml:"enableMediaInfo"`
	EnableNFO           bool          `json:"enableNfo" yaml:"enableNfo"`
	EnableBIF           bool          `json:"enableBif" yaml:"enableBif"`
	EnableImageTakeover bool          `json:"enableImageTakeover" yaml:"enableImageTakeover"`
}

type ScrapingConfig struct {
	EnableTMDB                   bool     `json:"enableTmdb" yaml:"enableTmdb"`
	EnablePeople                 bool     `json:"enablePeople" yaml:"enablePeople"`
	PreferOriginalLanguagePoster bool     `json:"preferOriginalLanguagePoster" yaml:"preferOriginalLanguagePoster"`
	ImageSources                 []string `json:"imageSources" yaml:"imageSources"`
	FanartAPIKey                 string   `json:"fanartApiKey" yaml:"fanartApiKey"`
	FanartBaseURL                string   `json:"fanartBaseUrl" yaml:"fanartBaseUrl"`
	TMDBAPIKey                   string   `json:"tmdbApiKey" yaml:"tmdbApiKey"`
	TMDBToken                    string   `json:"tmdbToken" yaml:"tmdbToken"`
	TMDBBaseURL                  string   `json:"tmdbBaseUrl" yaml:"tmdbBaseUrl"`
	Language                     string   `json:"language" yaml:"language"`
	FallbackLanguages            []string `json:"fallbackLanguages" yaml:"fallbackLanguages"`
	Region                       string   `json:"region" yaml:"region"`
	Proxy                        string   `json:"proxy" yaml:"proxy"`
}

type WatchDir struct {
	Path      string `json:"path" yaml:"path"`
	Recursive bool   `json:"recursive" yaml:"recursive"`
	Enabled   bool   `json:"enabled" yaml:"enabled"`
}

func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.applyDefaults()
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

func Default() Config {
	cfg := Config{}
	cfg.applyDefaults()
	return cfg
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = "127.0.0.1:18880"
	}
	if c.Server.Timezone == "" {
		c.Server.Timezone = "Asia/Shanghai"
	}
	if c.Database.Path == "" {
		c.Database.Path = "data/nyamedia.db"
	}
	if c.Processing.Concurrency <= 0 {
		c.Processing.Concurrency = 2
	}
	if c.Processing.StableDelay <= 0 {
		c.Processing.StableDelay = 30 * time.Second
	}
	if c.Processing.StableChecks <= 0 {
		c.Processing.StableChecks = 2
	}
	if c.Processing.BIFWidth <= 0 {
		c.Processing.BIFWidth = 320
	}
	if c.Processing.BIFInterval <= 0 {
		c.Processing.BIFInterval = 10
	}
	if c.Processing.BIFHWAccel == "" {
		c.Processing.BIFHWAccel = "cpu"
	}
	if len(c.Processing.Extensions) == 0 {
		c.Processing.Extensions = []string{".mkv", ".mp4", ".ts", ".m2ts", ".mts", ".mov", ".m4v", ".avi", ".wmv", ".flv", ".webm", ".rmvb", ".rm", ".mpg", ".mpeg", ".vob", ".asf"}
	}
	if c.WatchDirs == nil {
		c.WatchDirs = []WatchDir{}
	}
	if c.Scraping.Language == "" {
		c.Scraping.Language = "zh-CN"
	}
	if c.Scraping.Region == "" {
		c.Scraping.Region = "CN"
	}
	if c.Scraping.TMDBBaseURL == "" {
		c.Scraping.TMDBBaseURL = "https://api.themoviedb.org/3"
	}
	if c.Scraping.ImageSources == nil {
		c.Scraping.ImageSources = []string{"tmdb", "tvdb", "fanart"}
	}
	if c.Scraping.FanartBaseURL == "" {
		c.Scraping.FanartBaseURL = "https://webservice.fanart.tv/v3"
	}
}
