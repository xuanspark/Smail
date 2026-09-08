package runtimelog

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const legacyDirectory = "运行日志"

// migrateLegacyLogs 在日志写入和维护启动前迁移旧目录；不覆盖同名文件，不处理未知文件。
func migrateLegacyLogs(root *os.Root) error {
	info, err := root.Lstat(legacyDirectory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("旧运行日志目录必须是实际目录：%w", ErrInvalid)
	}
	legacy, err := root.OpenRoot(legacyDirectory)
	if err != nil {
		return err
	}
	defer legacy.Close()
	directory, err := legacy.Open(".")
	if err != nil {
		return err
	}
	opened, statErr := directory.Stat()
	entries, readErr := directory.ReadDir(-1)
	if err := errors.Join(statErr, readErr, directory.Close()); err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return fmt.Errorf("旧运行日志目录在迁移前发生变化：%w", ErrInvalid)
	}
	type sourceFile struct {
		name string
		info os.FileInfo
	}
	var sources []sourceFile
	unknown := false
	// 先检查全部源文件和目标，不能迁移到一半才发现已有同名日志。
	for _, entry := range entries {
		name := entry.Name()
		if _, ok := fileDay(name); !ok {
			unknown = true
			continue
		}
		source, err := legacy.Lstat(name)
		if err != nil {
			return err
		}
		if !source.Mode().IsRegular() {
			return fmt.Errorf("旧运行日志不是普通文件 %s：%w", name, ErrInvalid)
		}
		if _, err := root.Lstat(name); err == nil {
			return fmt.Errorf("运行日志迁移目标已存在，请核对两个目录的同名文件，拒绝覆盖：%s", name)
		} else if !os.IsNotExist(err) {
			return err
		}
		sources = append(sources, sourceFile{name: name, info: source})
	}
	for _, source := range sources {
		if err := moveLegacyLog(legacy, root, source.name, source.info); err != nil {
			return fmt.Errorf("运行日志迁移失败，未完成的源文件已保留 %s：%w", source.name, err)
		}
	}
	if unknown {
		return nil
	}
	if err := legacy.Close(); err != nil {
		return err
	}
	current, err := root.Lstat(legacyDirectory)
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("旧运行日志目录在迁移时发生变化：%w", ErrInvalid)
	}
	// 只移除迁移后的空目录；新出现的文件会使移除失败，绝不递归删除。
	return root.Remove(legacyDirectory)
}

// moveLegacyLog 使用排他创建和流式复制，兼容不支持硬链接的文件系统。
func moveLegacyLog(legacy, root *os.Root, name string, expected os.FileInfo) error {
	source, err := legacy.Open(name)
	if err != nil {
		return err
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil {
		return err
	}
	if !unchangedLog(expected, opened) {
		return ErrInvalid
	}
	target, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	created, err := target.Stat()
	complete := false
	defer func() {
		_ = target.Close()
		// 只清理本次创建且尚未复制完成的目标；中断或异常时源文件仍然存在。
		if !complete && created != nil {
			if current, err := root.Lstat(name); err == nil && os.SameFile(created, current) {
				_ = root.Remove(name)
			}
		}
	}()
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(target, source)
	after, statErr := source.Stat()
	if err := errors.Join(copyErr, statErr, target.Sync(), target.Close(), source.Close()); err != nil {
		return err
	}
	if written != expected.Size() || !unchangedLog(expected, after) {
		return fmt.Errorf("源日志在复制时发生变化：%w", ErrInvalid)
	}
	if err := root.Chtimes(name, expected.ModTime(), expected.ModTime()); err != nil {
		return err
	}
	current, err := legacy.Lstat(name)
	if err != nil {
		return err
	}
	if !unchangedLog(expected, current) {
		return fmt.Errorf("源日志在迁移时发生变化：%w", ErrInvalid)
	}
	destination, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !destination.Mode().IsRegular() || !os.SameFile(created, destination) || destination.Size() != written {
		return fmt.Errorf("目标日志在迁移时发生变化：%w", ErrInvalid)
	}
	// 目标已完整落盘；删除源失败时保留两份，重启明确报冲突，不猜测应保留哪份。
	complete = true
	return legacy.Remove(name)
}

func unchangedLog(expected, current os.FileInfo) bool {
	return current.Mode().IsRegular() && os.SameFile(expected, current) &&
		expected.Size() == current.Size() && expected.ModTime().Equal(current.ModTime())
}
