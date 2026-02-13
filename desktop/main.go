package main

import (
	"embed"

	"github.com/pt-nexus/server-go/desktop-go/internal/desktopapp"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	updateHandler, stopUpdater := desktopapp.NewUpdaterProxyHandler()
	defer stopUpdater()

	desktopHandler := desktopapp.NewRouteMux(desktopapp.RouteTargets{
		APIHandler:    desktopapp.NewServerGoAPIHandler(),
		UpdateHandler: updateHandler,
	})

	err := wails.Run(&options.App{
		Title:  "PT Nexus",
		Width:  1024,
		Height: 768,
		// 关闭窗口时仅隐藏，应用留在后台（通过托盘菜单“退出”真正结束）。
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: desktopHandler,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
