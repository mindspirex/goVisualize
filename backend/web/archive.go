package web

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxArchiveBytes = 100 << 20
const maxUnpackedBytes = 1 << 30
const maxExtractedBytes = 300 << 20
const maxFiles = 20000
const maxEntries = 100000

type downloadCounter struct {
	reader     io.Reader
	downloaded int64
	total      int64
	lastReport time.Time
	progress   func(int64, int64)
}

func (c *downloadCounter) Read(buffer []byte) (int, error) {
	n, err := c.reader.Read(buffer)
	c.downloaded += int64(n)
	if c.progress != nil && (c.lastReport.IsZero() || time.Since(c.lastReport) >= 250*time.Millisecond || err != nil) {
		c.progress(c.downloaded, c.total)
		c.lastReport = time.Now()
	}
	return n, err
}

func (g *githubClient) download(ctx context.Context, repo Repository, destination string, progress func(int64, int64)) error {
	address := "https://github.com/" + repo.Owner + "/" + repo.Repo + "/archive/" + repo.SHA + ".tar.gz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "govisualize")
	response, err := g.http.Do(request)
	if err != nil {
		return fmt.Errorf("download repository archive: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download repository archive: GitHub returned %s", response.Status)
	}
	if response.ContentLength > maxArchiveBytes {
		return fmt.Errorf("repository archive exceeds the 100 MiB download limit")
	}
	compressed := &downloadCounter{reader: io.LimitReader(response.Body, maxArchiveBytes+1), total: response.ContentLength, progress: progress}
	if progress != nil {
		progress(0, response.ContentLength)
	}
	archive, err := gzip.NewReader(compressed)
	if err != nil {
		if compressed.downloaded > maxArchiveBytes {
			return fmt.Errorf("repository archive exceeds the 100 MiB download limit")
		}
		return fmt.Errorf("open repository archive: %w", err)
	}
	defer archive.Close()
	unpacked := &io.LimitedReader{R: archive, N: maxUnpackedBytes + 1}
	err = extractTar(ctx, tar.NewReader(unpacked), destination)
	if progress != nil {
		progress(compressed.downloaded, compressed.total)
	}
	if compressed.downloaded > maxArchiveBytes {
		return fmt.Errorf("repository archive exceeds the 100 MiB download limit")
	}
	if unpacked.N == 0 {
		return fmt.Errorf("repository archive exceeds the 1 GiB unpacked content limit")
	}
	return err
}

func extractTar(ctx context.Context, reader *tar.Reader, destination string) error {
	var root string
	var total int64
	var unpacked int64
	files := 0
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read repository archive: %w", err)
		}
		entries++
		if entries > maxEntries {
			return fmt.Errorf("repository archive exceeds the 100,000 entry limit")
		}
		if header.Size < 0 {
			return fmt.Errorf("repository archive contains an invalid file size")
		}
		if header.Size > maxUnpackedBytes-unpacked {
			return fmt.Errorf("repository archive exceeds the 1 GiB unpacked content limit")
		}
		unpacked += header.Size
		name := path.Clean(header.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("repository archive contains an invalid path")
		}
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 1 || parts[1] == "" {
			continue
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return fmt.Errorf("repository archive has multiple roots")
		}
		target := filepath.Join(destination, filepath.FromSlash(parts[1]))
		relative, err := filepath.Rel(destination, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("repository archive contains an invalid path")
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if !analysisFile(parts[1]) {
				continue
			}
			files++
			if files > maxFiles {
				return fmt.Errorf("repository exceeds the 20,000 Go source and module file limit")
			}
			if header.Size > maxExtractedBytes-total {
				return fmt.Errorf("repository Go source and module files exceed the 300 MiB extracted size limit")
			}
			total += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func analysisFile(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == ".git" || part == "vendor" || part == "node_modules" {
			return false
		}
	}
	return path.Base(name) == "go.mod" || (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"))
}

func discoverModules(root string) ([]string, error) {
	modules := []string{}
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && filename != root && (entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() != "go.mod" {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(filename))
		if err != nil {
			return err
		}
		modules = append(modules, filepath.ToSlash(relative))
		if len(modules) > 100 {
			return fmt.Errorf("repository has more than 100 Go modules")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(modules)
	return modules, nil
}

func validModule(module string) bool {
	return module == "." || (module != "" && !path.IsAbs(module) && path.Clean(module) == module && module != ".." && !strings.HasPrefix(module, "../") && !strings.Contains(module, "\\"))
}
