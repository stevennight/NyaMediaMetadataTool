package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/metadataaudit"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	root := flag.String("root", "", "series root directory to check")
	tmdbShowID := flag.Int("tmdb-id", 0, "TMDB show id; overrides tvshow.nfo when set")
	embyItemURL := flag.String("emby-item-url", "", "Emby series item page URL")
	embyURL := flag.String("emby-url", "", "Emby server URL")
	embyAPIKey := flag.String("emby-api-key", "", "Emby API key")
	embySeriesID := flag.String("emby-series-id", "", "Emby series item id")
	jsonOutput := flag.Bool("json", false, "write JSON report")
	failOnIssue := flag.Bool("fail-on-issue", false, "exit with code 2 when missing episodes, warnings or Emby differences are found")
	flag.Parse()

	if *root == "" {
		fmt.Fprintln(os.Stderr, "-root is required")
		os.Exit(1)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := metadataaudit.Run(ctx, metadataaudit.Options{
		Root:         *root,
		Config:       cfg,
		TMDBShowID:   *tmdbShowID,
		EmbyItemURL:  *embyItemURL,
		EmbyURL:      *embyURL,
		EmbyAPIKey:   *embyAPIKey,
		EmbySeriesID: *embySeriesID,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *jsonOutput {
		err = metadataaudit.WriteJSON(os.Stdout, report)
	} else {
		err = metadataaudit.WriteText(os.Stdout, report)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *failOnIssue && metadataaudit.HasIssues(report) {
		os.Exit(2)
	}
}
