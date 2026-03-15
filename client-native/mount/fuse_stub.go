//go:build !linux

package mount

import "fmt"

// FuseMount — stub pro platformy bez FUSE.
type FuseMount struct {
	mountPath string
}

func MountFUSE(vfs VirtualFS, mountPath string) (*FuseMount, error) {
	return nil, fmt.Errorf("FUSE mount is only supported on Linux")
}

func (m *FuseMount) Unmount() error {
	return nil
}

func (m *FuseMount) Path() string {
	if m == nil {
		return ""
	}
	return m.mountPath
}

func DefaultMountPath(serverAddr, shareName string) string {
	return ""
}
