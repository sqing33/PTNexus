//go:build !linux && !windows

package media

import "fmt"

func openISOSession(isoPath string, scene string) (*MediaSession, error) {
	return nil, fmt.Errorf("当前平台暂不支持 ISO 自动挂载: scene=%s path=%s", scene, isoPath)
}
