//go:build windows

package tray

import (
	"context"
	_ "embed"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// 托盘图标必须是 Windows 可识别的 .ico 格式。
//
//go:embed icon.ico
var trayIcon []byte

type windowsService struct {
	ctx context.Context

	onceStart sync.Once
	onceStop  sync.Once
	stopCh    chan struct{}
	runDone   chan struct{}
}

func newService() Service {
	return &windowsService{
		stopCh:  make(chan struct{}),
		runDone: make(chan struct{}),
	}
}

func (s *windowsService) Start(ctx context.Context) {
	s.onceStart.Do(func() {
		s.ctx = ctx
		go func() {
			goruntime.LockOSThread()
			defer goruntime.UnlockOSThread()
			defer close(s.runDone)
			systray.Run(s.onReady, s.onExit)
		}()
	})
}

func (s *windowsService) Stop() {
	s.onceStop.Do(func() {
		close(s.stopCh)
		systray.Quit()
		select {
		case <-s.runDone:
		case <-time.After(2 * time.Second):
		}
	})
}

func (s *windowsService) onReady() {
	if len(trayIcon) > 0 {
		systray.SetIcon(trayIcon)
	}
	// 提示文本
	systray.SetTitle("PT Nexus")
	systray.SetTooltip("PT Nexus")

	mShow := systray.AddMenuItem("显示主窗口", "显示 PT Nexus")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出 PT Nexus")

	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				if s.ctx != nil {
					ctx := s.ctx
					go func() {
						runtime.WindowUnminimise(ctx)
						runtime.WindowShow(ctx)
					}()
				}
			case <-mQuit.ClickedCh:
				if s.ctx != nil {
					ctx := s.ctx
					go runtime.Quit(ctx)
				} else {
					systray.Quit()
				}
				return
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *windowsService) onExit() {
	// systray 退出时由 Wails 生命周期收尾。
}
