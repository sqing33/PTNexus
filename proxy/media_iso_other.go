//go:build !linux && !windows

package main

import "fmt"

func openISOSession(isoPath string, scene string) (*MediaSession, error) {
	return nil, fmt.Errorf("automatic ISO mounting is not supported on this platform: scene=%s path=%s", scene, isoPath)
}
