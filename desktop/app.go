package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/pt-nexus/server/desktop/internal/desktopapp"
	"github.com/pt-nexus/server/desktop/internal/tray"
)

// App struct
type App struct {
	ctx         context.Context
	trayService tray.Service
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		trayService: tray.New(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.trayService != nil {
		a.trayService.Start(ctx)
	}
}

// shutdown 在应用退出时调用，用于托盘资源清理。
func (a *App) shutdown(ctx context.Context) {
	a.ctx = ctx
	if a.trayService != nil {
		a.trayService.Stop()
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// GetDatabaseConfigFilePath 返回桌面端数据库配置文件路径。
func (a *App) GetDatabaseConfigFilePath() string {
	return desktopapp.DatabaseConfigFilePath()
}

// OpenDatabaseConfigFile 使用系统默认程序打开桌面端 database.json。
func (a *App) OpenDatabaseConfigFile() error {
	desktopapp.EnsureDatabaseConfigFile()
	path := desktopapp.DatabaseConfigFilePath()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
