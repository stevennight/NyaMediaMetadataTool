package store

import (
	"context"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/config"
)

func TestWatchDirProcessingRoundTripAndLongestPathMatch(t *testing.T) {
	t.Parallel()

	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "media")
	child := filepath.Join(root, "anime")
	if _, err := st.CreateWatchDir(context.Background(), WatchDir{
		Path:                root,
		Recursive:           true,
		WatchEnabled:        true,
		UseGlobalProcessing: true,
		Processing:          config.Default().Processing.OutputConfig(),
	}); err != nil {
		t.Fatal(err)
	}
	independent := config.Default().Processing.OutputConfig()
	independent.Strategy = config.ProcessingStrategyForce
	independent.EnableBIF = false
	created, err := st.CreateWatchDir(context.Background(), WatchDir{
		Path:                child,
		Recursive:           true,
		WatchEnabled:        true,
		UseGlobalProcessing: false,
		Processing:          independent,
	})
	if err != nil {
		t.Fatal(err)
	}

	matched, err := st.FindWatchDirForPath(context.Background(), filepath.Join(child, "show", "episode.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if matched.ID != created.ID {
		t.Fatalf("expected longest matching directory %d, got %d", created.ID, matched.ID)
	}
	if matched.UseGlobalProcessing {
		t.Fatal("expected independent processing settings")
	}
	if matched.Processing.Strategy != config.ProcessingStrategyForce || matched.Processing.EnableBIF {
		t.Fatalf("unexpected processing settings: %+v", matched.Processing)
	}
}
