package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	maxFileSize  = 50 << 20  // 50 MB per file
	maxTotalSize = 200 << 20 // 200 MB total extraction limit
)

func downloadChart(repo, ref, chartPath, outputDir string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/tarball/%s", repo, ref)

	body, err := githubGetRaw(url)
	if err != nil {
		return err
	}
	defer body.Close()

	return extractChart(body, chartPath, outputDir)
}

func extractChart(r io.Reader, chartPath, outputDir string) error {
	// MkdirAll/OpenFile's mode argument is masked by the process umask, so a
	// restrictive umask (some CI runners and hardened shells set one) would
	// otherwise silently produce directories/files more restrictive than the
	// 0755/0644 requested below. Zeroing the umask for the duration of
	// extraction makes the requested modes apply exactly, regardless of the
	// caller's environment, without needing to chmod anything after the fact.
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	prefix := chartPath + "/"
	tr := tar.NewReader(gz)
	found := false
	var totalSize int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// GitHub tarballs have a top-level dir like "Org-Repo-SHA/"
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		relPath := parts[1]

		if !strings.HasPrefix(relPath, prefix) {
			continue
		}
		found = true

		innerPath := strings.TrimPrefix(relPath, prefix)

		cleaned := filepath.Clean(innerPath)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid path in tarball: %s", relPath)
		}

		targetPath := filepath.Join(outputDir, cleaned)

		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("unsupported tar entry type (symlink) for %s", relPath)
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("creating dir %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			if hdr.Size > maxFileSize {
				return fmt.Errorf("file %s exceeds maximum size (%d > %d)", relPath, hdr.Size, maxFileSize)
			}
			totalSize += hdr.Size
			if totalSize > maxTotalSize {
				return fmt.Errorf("archive exceeds total extraction limit (%d > %d)", totalSize, maxTotalSize)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("creating parent dir for %s: %w", targetPath, err)
			}
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFileSize)); err != nil {
				f.Close()
				return fmt.Errorf("writing file %s: %w", targetPath, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("closing file %s: %w", targetPath, err)
			}
		}
	}

	if !found {
		return fmt.Errorf("chart path %q not found in tarball", chartPath)
	}
	return nil
}
