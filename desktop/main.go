package main

import (
	"embed"
	"fmt"
	"os"

	desktopapp "github.com/grok-free-register/grok-reg/desktop/app"
	"github.com/grok-free-register/grok-reg/internal/daemon"
	"github.com/grok-free-register/grok-reg/internal/runner"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "0.1.0"

func main() {
	if daemon.IsWorker() {
		if err := runner.RunWorker(os.Args[1:]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "worker error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	service := desktopapp.New(version)
	app := application.New(application.Options{
		Name:        "GRF",
		Description: "GRF registration control center",
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "GRF",
		Width:            1240,
		Height:           780,
		MinWidth:         960,
		MinHeight:        620,
		BackgroundColour: application.NewRGB(246, 247, 249),
		URL:              "/",
		Frameless:        true,
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: false,
			NonClientRegionSupport:            true,
		},
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHidden,
			InvisibleTitleBarHeight: 48,
		},
	})

	if err := app.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "desktop error: %v\n", err)
		os.Exit(1)
	}
}
