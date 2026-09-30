//go:build linux

package hooks

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	pluginAddressSpaceLimit = 2 << 30
	pluginFileLimit         = 128
	pluginProcessLimit      = 32
)

func newPluginLaunchSpec(pluginPath, root, name, socketName string, env []string) (*pluginLaunchSpec, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("the Smail process must run as root to isolate plugins")
	}
	uid, err := pluginIdentity("SMAIL_PLUGIN_UID")
	if err != nil {
		return nil, err
	}
	gid, err := pluginIdentity("SMAIL_PLUGIN_GID")
	if err != nil {
		return nil, err
	}
	if uid == 0 || gid == 0 {
		return nil, errors.New("the plugin identity must not be root")
	}

	runtimeDir, err := os.MkdirTemp("", "smail-plugin-")
	if err != nil {
		return nil, fmt.Errorf("create plugin sandbox: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(runtimeDir) }
	if err := unix.Chmod(runtimeDir, 0700); err != nil {
		cleanup()
		return nil, fmt.Errorf("secure plugin sandbox: %w", err)
	}
	if err := os.Chown(runtimeDir, uid, gid); err != nil {
		cleanup()
		return nil, fmt.Errorf("assign plugin sandbox identity: %w", err)
	}

	sandboxPluginPath := filepath.Join(runtimeDir, name)
	if err := copyPluginExecutable(pluginPath, sandboxPluginPath, uid, gid); err != nil {
		cleanup()
		return nil, err
	}
	if err := copyPluginConfig(root, runtimeDir, name, uid, gid); err != nil {
		cleanup()
		return nil, err
	}

	isolatedEnv := isolatedPluginEnv(env)
	abstractSocket := "@" + socketName
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: abstractSocket, Net: "unix"})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create plugin listener: %w", err)
	}
	listenerFile, err := listener.File()
	if closeErr := listener.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("prepare plugin listener: %w", err)
	}
	cloneFlags := uintptr(unix.CLONE_NEWNET)
	spec := &pluginLaunchSpec{
		path:        "/" + name,
		args:        []string{name, "fd:3"},
		socketPath:  abstractSocket,
		runtimeDir:  runtimeDir,
		parentFiles: []*os.File{listenerFile},
		procAttr: &os.ProcAttr{
			Dir: "/",
			Env: isolatedEnv,
			Files: []*os.File{
				os.Stdin,
				os.Stdout,
				os.Stderr,
				listenerFile,
			},
			Sys: &syscall.SysProcAttr{
				Chroot: runtimeDir,
				Credential: &syscall.Credential{
					Uid:         uint32(uid), // #nosec G115 -- pluginIdentity restricts both values to 1..65534.
					Gid:         uint32(gid), // #nosec G115 -- pluginIdentity restricts both values to 1..65534.
					NoSetGroups: true,
				},
				Cloneflags: cloneFlags,
				Pdeathsig:  syscall.SIGKILL,
				Setsid:     true,
			},
		},
	}
	if err := startPluginEgress(spec, uid, gid); err != nil {
		spec.cleanup()
		return nil, err
	}
	return spec, nil
}

func copyPluginConfig(root, runtimeDir, name string, uid, gid int) error {
	configPath := filepath.Join(root, pluginConfigFileName(name))
	info, err := os.Lstat(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSetuid != 0 || info.Mode()&os.ModeSetgid != 0 || info.Size() > maxPluginConfigBytes {
		return errors.New("plugin configuration file is unsafe")
	}
	if !securePluginFileOwner(info) {
		return errors.New("plugin configuration file owner is unsafe")
	}
	configDir := filepath.Join(runtimeDir, "plugins")
	if err := os.Mkdir(configDir, 0700); err != nil {
		return fmt.Errorf("create plugin configuration directory: %w", err)
	}
	if err := os.Chown(configDir, uid, gid); err != nil {
		return fmt.Errorf("assign plugin configuration directory identity: %w", err)
	}
	return copyPluginFile(configPath, filepath.Join(configDir, filepath.Base(configPath)), uid, gid, 0600)
}

func copyPluginFile(sourcePath, targetPath string, uid, gid int, mode os.FileMode) error {
	source, err := openPluginSource(sourcePath)
	if err != nil {
		return fmt.Errorf("open plugin configuration: %w", err)
	}
	defer source.Close()
	target, err := createPluginTarget(targetPath, mode)
	if err != nil {
		return fmt.Errorf("create sandbox plugin configuration: %w", err)
	}
	written, err := io.Copy(target, io.LimitReader(source, maxPluginConfigBytes+1))
	if err != nil {
		_ = target.Close()
		_ = os.Remove(targetPath)
		return fmt.Errorf("copy plugin configuration: %w", err)
	}
	if written > maxPluginConfigBytes {
		_ = os.Remove(targetPath)
		return errors.New("plugin configuration exceeds the allowed size")
	}
	if err := target.Chmod(mode); err != nil {
		_ = target.Close()
		_ = os.Remove(targetPath)
		return fmt.Errorf("secure sandbox plugin configuration: %w", err)
	}
	if err := target.Chown(uid, gid); err != nil {
		_ = target.Close()
		_ = os.Remove(targetPath)
		return fmt.Errorf("assign sandbox plugin configuration identity: %w", err)
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("close sandbox plugin configuration: %w", err)
	}
	return nil
}

func openPluginSource(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open plugin source returned an invalid file")
	}
	return file, nil
}

func createPluginTarget(path string, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("create plugin target returned an invalid file")
	}
	return file, nil
}

func pluginIdentity(name string) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, fmt.Errorf("%s must be configured for production plugins", name)
	}
	identity, err := strconv.Atoi(value)
	if err != nil || identity < 1 || identity > 65534 {
		return 0, fmt.Errorf("%s is invalid", name)
	}
	return identity, nil
}

func copyPluginExecutable(sourcePath, targetPath string, uid, gid int) error {
	source, err := openPluginSource(sourcePath)
	if err != nil {
		return fmt.Errorf("open plugin executable: %w", err)
	}
	defer source.Close()

	target, err := createPluginTarget(targetPath, 0700)
	if err != nil {
		return fmt.Errorf("create sandbox plugin executable: %w", err)
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return fmt.Errorf("copy plugin executable: %w", err)
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		return fmt.Errorf("sync sandbox plugin executable: %w", err)
	}
	if err := target.Chmod(0700); err != nil {
		_ = target.Close()
		_ = os.Remove(targetPath)
		return fmt.Errorf("secure sandbox plugin executable: %w", err)
	}
	if err := target.Chown(uid, gid); err != nil {
		_ = target.Close()
		_ = os.Remove(targetPath)
		return fmt.Errorf("assign sandbox plugin identity: %w", err)
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("close sandbox plugin executable: %w", err)
	}
	return nil
}

func securePluginDirectoryOwner(root string) bool {
	info, err := os.Lstat(root)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func securePluginFileOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func applyPluginResourceLimits(process *os.Process) error {
	if process == nil {
		return errors.New("plugin process is unavailable")
	}
	limits := []struct {
		resource int
		value    uint64
	}{
		{resource: unix.RLIMIT_AS, value: pluginAddressSpaceLimit},
		{resource: unix.RLIMIT_NOFILE, value: pluginFileLimit},
		{resource: unix.RLIMIT_NPROC, value: pluginProcessLimit},
	}
	for _, limit := range limits {
		rlimit := &unix.Rlimit{Cur: limit.value, Max: limit.value}
		if err := unix.Prlimit(process.Pid, limit.resource, rlimit, nil); err != nil {
			return fmt.Errorf("set plugin resource limit: %w", err)
		}
	}
	return nil
}
