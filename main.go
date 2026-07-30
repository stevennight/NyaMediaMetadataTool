package main

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"NyaMediaMetadataTool/internal/appdata"
	"NyaMediaMetadataTool/web"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

const desktopProductName = "NyaMediaMetadataTool"

var (
	version          = "dev"
	commit           = "unknown"
	buildDate        = "unknown"
	updateRepository = "stevennight/NyaMediaMetadataTool"
)

func main() {
	if err := runDesktop(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func runDesktop() error {
	paths, err := appdata.DefaultPaths()
	if err != nil {
		return err
	}
	logger, closeLog, err := desktopLogger(paths)
	if err != nil {
		return err
	}
	defer closeLog()

	desktop := NewDesktopApp(paths, version, logger)
	desktop.startHidden = launchedInBackground(os.Args[1:])
	defer desktop.closeService()

	err = wails.Run(&options.App{
		Title:             desktopProductName,
		Width:             1440,
		Height:            900,
		MinWidth:          900,
		MinHeight:         640,
		BackgroundColour:  options.NewRGB(246, 247, 249),
		StartHidden:       true,
		HideWindowOnClose: desktopTraySupported(),
		AssetServer: &assetserver.Options{
			Assets:  web.Assets(),
			Handler: desktop.Handler(web.Handler()),
		},
		OnStartup:     desktop.startup,
		OnDomReady:    desktop.domReady,
		OnBeforeClose: desktop.beforeClose,
		OnShutdown:    desktop.shutdown,
		Bind: []interface{}{
			desktop,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "com.nyamedia.metadata-tool",
			OnSecondInstanceLaunch: desktop.secondInstance,
		},
		DragAndDrop: &options.DragAndDrop{
			DisableWebViewDrop: true,
		},
		Windows: &windows.Options{
			Theme:               windows.SystemDefault,
			WebviewUserDataPath: filepath.Join(paths.Root, "webview"),
			ResizeDebounceMS:    8,
		},
		Mac: &mac.Options{
			Appearance: mac.DefaultAppearance,
			TitleBar: &mac.TitleBar{
				HideToolbarSeparator: true,
			},
			DisableEscapeExitsFullscreen: true,
		},
		Linux: &linux.Options{
			ProgramName:      "nya-media-metadata-tool",
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		logger.Error("run desktop application", "error", err)
		return err
	}
	return nil
}

func launchedInBackground(args []string) bool {
	for _, arg := range args {
		if arg == "--background" {
			return true
		}
	}
	return false
}

func desktopLogger(paths appdata.Paths) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(paths.Logs, 0o755); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(filepath.Join(paths.Logs, "nyamedia.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	var console io.Writer
	if desktopOutputAvailable(os.Stdout) {
		console = os.Stdout
	}
	handler := slog.NewTextHandler(desktopLogWriter(file, console), &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler), func() { _ = file.Close() }, nil
}

func desktopLogWriter(file io.Writer, console io.Writer) io.Writer {
	if console == nil {
		return file
	}
	return io.MultiWriter(file, console)
}

func desktopOutputAvailable(output *os.File) bool {
	if output == nil {
		return false
	}
	_, err := output.Stat()
	return err == nil
}
