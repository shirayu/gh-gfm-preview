package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ParseExtensions parses a comma-separated string of file extensions
func ParseExtensions(extensionsStr string) []string {
	if extensionsStr == "" {
		return []string{".md"}
	}

	parts := strings.Split(extensionsStr, ",")
	extensions := make([]string, 0, len(parts))

	for _, ext := range parts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		// Add leading dot if not present
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extensions = append(extensions, strings.ToLower(ext))
	}

	if len(extensions) == 0 {
		return []string{".md"}
	}

	return extensions
}

// ListMarkdownFiles recursively lists files with specified extensions in a directory
func ListMarkdownFiles(dir string, extensions []string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Check if file has one of the specified extensions (case-insensitive)
		ext := strings.ToLower(filepath.Ext(path))
		for _, validExt := range extensions {
			if ext == validExt {
				// Get relative path from dir
				relPath, err := filepath.Rel(dir, path)
				if err != nil {
					return err
				}
				files = append(files, relPath)
				break
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %w", err)
	}

	// Sort files alphabetically
	sort.Strings(files)

	return files, nil
}

// ListDirectoryContents lists only the immediate contents (files and directories) of a directory
func ListDirectoryContents(dir string, extensions []string) (files []string, dirs []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		} else {
			// Check if file has one of the specified extensions (case-insensitive)
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			for _, validExt := range extensions {
				if ext == validExt {
					files = append(files, entry.Name())
					break
				}
			}
		}
	}

	// Sort alphabetically
	sort.Strings(files)
	sort.Strings(dirs)

	return files, dirs, nil
}

// HasAllowedExtension checks if a file path has one of the allowed extensions
func HasAllowedExtension(filePath string, extensions []string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, validExt := range extensions {
		if ext == validExt {
			return true
		}
	}
	return false
}
