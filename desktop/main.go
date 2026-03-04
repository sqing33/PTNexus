package main

import (
	"embed"

	"github.com/pt-nexus/server/desktop/internal/desktopapp"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	indexHTML, _ := assets.ReadFile("frontend/dist/index.html")

	desktopHandler := desktopapp.NewRouteMux(desktopapp.RouteTargets{
		IndexHTML: indexHTML,
	})

	err := wails.Run(&options.App{
		Title:  "PT Nexus",
		Width:  1024,
		Height: 768,
		// 桌面端默认启动即最大化，避免首次进入时视口过小影响引导流程。
		WindowStartState: options.Maximised,
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
