package backup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	skillstore "racomi/internal/skills"
)

const maxArchiveBytes = 128 << 20
const maxExtractBytes = 512 << 20

// Source catalogs are metadata and can be larger than the installed store.
const maxManifestBytes = 16 << 20

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type manifest struct {
	Format    string           `json:"format"`
	Version   int              `json:"version"`
	CreatedAt string           `json:"createdAt"`
	State     skillstore.State `json:"state"`
}

// Export writes a portable backup. Target paths and deployments stay on this machine.
func Export(root string, state skillstore.State, destination string) (err error) {
	if !filepath.IsAbs(destination) || !strings.EqualFold(filepath.Ext(destination), ".zip") {
		return errors.New("请选择以 .zip 结尾的绝对备份路径")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	if inside(root, destination) {
		return errors.New("备份文件须保存在 racomi 数据目录之外")
	}
	if info, statErr := os.Lstat(destination); statErr == nil && !info.Mode().IsRegular() {
		return errors.New("备份路径不是普通文件")
	}
	for _, target := range state.Targets {
		paths := append([]string{target.Path}, target.Paths...)
		for _, deployment := range target.Deployments {
			if deployment.Path != "" {
				paths = append(paths, deployment.Path)
			}
		}
		for _, targetPath := range paths {
			if targetPath != "" && inside(targetPath, destination) {
				return errors.New("备份文件须保存在技能目标目录之外")
			}
		}
	}
	state.Targets = []skillstore.Target{}
	contents, err := json.MarshalIndent(manifest{"racomi-backup", 1, time.Now().UTC().Format(time.RFC3339), state}, "", "  ")
	if err != nil {
		return err
	}
	if len(contents) > maxManifestBytes {
		return errors.New("备份清单超过 16 MiB")
	}
	fileCount := 1
	var expanded int64
	file, err := os.CreateTemp(filepath.Dir(destination), ".racomi-export-*.zip")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { file.Close(); os.Remove(temp) }()
	writer := zip.NewWriter(file)
	entry, err := writer.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err = entry.Write(contents); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, skill := range state.Skills {
		hash := skill.ContentHash
		if !hashPattern.MatchString(hash) {
			return errors.New("技能内容版本无效")
		}
		if seen[hash] {
			continue
		}
		seen[hash] = true
		object := filepath.Join(root, "objects", hash)
		actual, err := skillstore.HashDir(object)
		if err != nil {
			return err
		}
		if actual != hash {
			return fmt.Errorf("技能 %s 的内容校验失败", skill.Name)
		}
		err = filepath.WalkDir(object, func(filename string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filename == object {
				return nil
			}
			fileCount++
			if fileCount > 10000 {
				return errors.New("备份文件数量超过上限")
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("备份不支持符号链接或特殊文件")
			}
			if info.Mode().IsRegular() {
				expanded += info.Size()
				if expanded > maxExtractBytes {
					return errors.New("备份内容超过 512 MiB")
				}
			}
			relative, err := filepath.Rel(object, filename)
			if err != nil {
				return err
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = "objects/" + hash + "/" + filepath.ToSlash(relative)
			if item.IsDir() {
				header.Name += "/"
			} else {
				header.Method = zip.Deflate
			}
			out, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			if item.IsDir() {
				return nil
			}
			in, err := os.Open(filename)
			if err != nil {
				return err
			}
			defer in.Close()
			_, err = io.Copy(out, in)
			return err
		})
		if err != nil {
			return err
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	if info, statErr := file.Stat(); statErr != nil {
		return statErr
	} else if info.Size() > maxArchiveBytes {
		return errors.New("备份压缩后超过 128 MiB")
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, destination)
}

// Import validates archive paths and content before restoring the store.
func Import(store *skillstore.Store, filename string) error {
	releaseContent := store.SuspendContentCleanup()
	defer releaseContent()
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxArchiveBytes {
		return errors.New("备份必须是小于 128 MiB 的普通 ZIP 文件")
	}
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("无法打开备份：%w", err)
	}
	defer reader.Close()
	if len(reader.File) > 10000 {
		return errors.New("备份文件数量超过上限")
	}
	var data manifest
	found := false
	for _, file := range reader.File {
		if file.Name != "manifest.json" {
			continue
		}
		if found {
			return errors.New("备份中有重复清单")
		}
		found = true
		in, err := file.Open()
		if err != nil {
			return err
		}
		bytes, err := io.ReadAll(io.LimitReader(in, maxManifestBytes+1))
		in.Close()
		if err != nil {
			return err
		}
		if len(bytes) > maxManifestBytes {
			return errors.New("备份清单过大")
		}
		if err = json.Unmarshal(bytes, &data); err != nil {
			return fmt.Errorf("备份清单无效：%w", err)
		}
	}
	if !found || data.Format != "racomi-backup" || data.Version != 1 || (data.State.Schema != 1 && data.State.Schema != 2) {
		return errors.New("不是支持的 racomi 备份格式")
	}
	if len(data.State.Targets) != 0 {
		return errors.New("便携备份不得包含本机目标路径")
	}
	hashes := map[string]bool{}
	for _, skill := range data.State.Skills {
		if !hashPattern.MatchString(skill.ContentHash) {
			return errors.New("备份中的内容版本无效")
		}
		hashes[skill.ContentHash] = true
	}
	stage, err := os.MkdirTemp(store.Root(), ".import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	seen := map[string]bool{}
	var total int64
	for _, file := range reader.File {
		name := file.Name
		if name == "manifest.json" {
			continue
		}
		trimmed := strings.TrimSuffix(name, "/")
		if strings.Contains(name, "\\") || path.IsAbs(name) || path.Clean(trimmed) != trimmed {
			return errors.New("备份含有不安全的文件路径")
		}
		parts := strings.Split(trimmed, "/")
		if len(parts) < 2 || parts[0] != "objects" || !hashes[parts[1]] {
			return errors.New("备份含有清单之外的文件")
		}
		if seen[trimmed] {
			return errors.New("备份含有重复文件")
		}
		seen[trimmed] = true
		if file.Mode()&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return errors.New("备份不支持符号链接或特殊文件")
		}
		if file.UncompressedSize64 > uint64(maxExtractBytes-total) {
			return errors.New("备份解压内容超过 512 MiB")
		}
		destination := filepath.Join(stage, filepath.FromSlash(trimmed))
		if !inside(stage, destination) {
			return errors.New("备份文件路径越界")
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0700); err != nil {
				return err
			}
			continue
		}
		if len(parts) < 3 {
			return errors.New("备份对象必须是目录")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600|(file.Mode().Perm()&0111))
		if err != nil {
			in.Close()
			return err
		}
		if err := out.Chmod(0600 | (file.Mode().Perm() & 0111)); err != nil {
			out.Close()
			in.Close()
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(in, maxExtractBytes-total+1))
		closeErr := out.Close()
		in.Close()
		total += written
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if total > maxExtractBytes {
			return errors.New("备份解压内容超过 512 MiB")
		}
	}
	for hash := range hashes {
		object := filepath.Join(stage, "objects", hash)
		actual, err := skillstore.HashDir(object)
		if err != nil || actual != hash {
			return fmt.Errorf("备份对象 %s 校验失败", hash)
		}
		if _, err = os.Stat(filepath.Join(object, "SKILL.md")); err != nil {
			return errors.New("备份对象缺少 SKILL.md")
		}
	}
	for hash := range hashes {
		destination := filepath.Join(store.Root(), "objects", hash)
		if _, err := os.Lstat(destination); err == nil {
			actual, err := skillstore.HashDir(destination)
			if err != nil || actual != hash {
				return errors.New("本地内容对象校验失败，未覆盖")
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(filepath.Join(stage, "objects", hash), destination); err != nil {
			return err
		}
	}
	return store.Restore(data.State)
}

func inside(parent, child string) bool {
	// macOS and Windows commonly use case-insensitive volumes. Treat case
	// aliases as protected even on a case-sensitive volume on those hosts.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		parent, child = strings.ToLower(parent), strings.ToLower(child)
	}
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
