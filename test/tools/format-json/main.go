// 本工具仅在停服后调整指定配置目录的空白排版，并同步原有文件摘要。
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	directory := flag.String("config", "", "已停止后端的配置目录")
	backup := flag.String("backup", "", "保存原字节的独立备份目录")
	apply := flag.Bool("apply", false, "确认停服，执行排版；默认只检查")
	flag.Parse()
	if !filepath.IsAbs(*directory) || !filepath.IsAbs(*backup) {
		return errors.New("配置及备份目录必须使用明确的绝对路径")
	}
	info, err := os.Lstat(*directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("配置目录不可用或是链接")
	}
	names := []string{"smtp.json", "email.json", "ssl.json", "plugins.json", "config.json"}
	original, formatted := map[string][]byte{}, map[string][]byte{}
	for _, name := range names {
		path := filepath.Join(*directory, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return errors.New("配置文件不可用、过大或不是普通文件")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var result bytes.Buffer
		// 不解析再编码业务值，保留中文、密文、数字及转义的原始字节。
		if err := json.Indent(&result, data, "", "  "); err != nil {
			return fmt.Errorf("%s 不是有效 JSON，未写入", name)
		}
		original[name], formatted[name] = data, result.Bytes()
	}
	var root struct {
		Sections map[string]struct {
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(original["config.json"], &root); err != nil || len(root.Sections) != 4 {
		return errors.New("仅支持已完成分区的五文件配置")
	}
	for _, name := range names[:4] {
		reference := root.Sections[name[:len(name)-5]]
		if reference.File != name || reference.SHA256 != digest(original[name]) {
			return fmt.Errorf("%s 原有摘要不匹配，未写入", name)
		}
		old := []byte(`"sha256": "` + reference.SHA256 + `"`)
		next := []byte(`"sha256": "` + digest(formatted[name]) + `"`)
		if bytes.Count(formatted["config.json"], old) != 1 {
			return errors.New("根配置摘要位置不唯一，未写入")
		}
		formatted["config.json"] = bytes.Replace(formatted["config.json"], old, next, 1)
	}
	changed := false
	for _, name := range names {
		changed = changed || !bytes.Equal(original[name], formatted[name])
	}
	if !*apply || !changed {
		fmt.Printf("检查通过：五个 JSON 可读取，是否需要排版：%t。\n", changed)
		return nil
	}
	// 备份目录必须不存在，拒绝覆盖之前的备份。
	if err := os.Mkdir(*backup, 0700); err != nil {
		return errors.New("无法创建独立备份目录，未改写配置")
	}
	for _, name := range names {
		if err := atomicfile.Write(filepath.Join(*backup, name), original[name], 0600); err != nil {
			return errors.New("备份未完成，未改写配置")
		}
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(*directory, name))
		if err != nil || !bytes.Equal(data, original[name]) {
			return errors.New("备份期间配置发生变化，未改写配置")
		}
	}
	for _, name := range names {
		if err := atomicfile.Write(filepath.Join(*directory, name), formatted[name], 0600); err != nil {
			return errors.New("写入未完成，请保持停服并使用本次备份恢复五个文件")
		}
	}
	fmt.Println("五个 JSON 已分行缩进，摘要已同步；配置值、代次及密文未改变。")
	return nil
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}
