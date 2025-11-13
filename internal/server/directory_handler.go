package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/thiagokokada/gh-gfm-preview/internal/app"
	"github.com/thiagokokada/gh-gfm-preview/internal/utils"
)

// handleDirectoryMode handles HTTP requests in directory browsing mode
func handleDirectoryMode(w http.ResponseWriter, r *http.Request, param *Param) {
	// Directory mode - extract current path from URL
	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	urlPath = strings.TrimSuffix(urlPath, "/")

	extensions := app.ParseExtensions(param.DirectoryListingShowExtensions)
	textExtensions := app.ParseExtensions(param.DirectoryListingTextExtensions)

	// Determine the actual filesystem path
	var currentDir string
	var currentURLPath string
	if urlPath == "" {
		currentDir = param.DirectoryPath
		currentURLPath = ""
	} else {
		currentDir = filepath.Join(param.DirectoryPath, urlPath)
		currentURLPath = urlPath
	}

	// Security check: ensure currentDir is within param.DirectoryPath
	absBase, err := filepath.Abs(param.DirectoryPath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	absCurrent, err := filepath.Abs(currentDir)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Ensure absBase ends with separator for proper prefix checking
	if !strings.HasSuffix(absBase, string(filepath.Separator)) {
		absBase += string(filepath.Separator)
	}

	// Check if current path is within base directory
	if absCurrent != strings.TrimSuffix(absBase, string(filepath.Separator)) && !strings.HasPrefix(absCurrent+string(filepath.Separator), absBase) {
		utils.LogDebugf("Path traversal attempt: base=%s, current=%s", absBase, absCurrent)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Check if it's a file request
	info, err := os.Stat(currentDir)
	isFile := err == nil && !info.IsDir()

	if isFile {
		// File access - check if extension is allowed
		if !app.HasAllowedExtension(currentDir, extensions) {
			http.Error(w, "Forbidden: File type not allowed", http.StatusForbidden)
			return
		}

		// Check if file is a text file - if not, let browser handle it natively
		if !app.IsTextFile(currentDir, textExtensions) {
			// Serve file directly using http.ServeFile (binary file)
			http.ServeFile(w, r, currentDir)
			return
		}

		// Text file - render with markdown template
		templateParam := TemplateParam{
			Title:            getTitle(currentDir),
			Body:             mdResponse(w, currentDir, param),
			Host:             r.Host,
			Reload:           param.Reload,
			Mode:             param.getMode().String(),
			ShowBrowseButton: true,
			IsDirectoryIndex: false,
			HasReadme:        false,
			CurrentPath:      currentURLPath,
			ParentPath:       getParentPath(currentURLPath),
		}

		err := tmpl.Execute(w, templateParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	// Directory access
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Check for README in current directory
	readme, readmeErr := app.FindReadme(currentDir)
	viewMode := r.URL.Query().Get("view")

	// Show directory listing
	if viewMode == "index" || readmeErr != nil {
		files, dirs, err := app.ListDirectoryContents(currentDir, extensions)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error listing directory: %v", err), http.StatusInternalServerError)
			return
		}

		templateParam := TemplateParam{
			Title:            "Browse Files",
			Body:             "",
			Host:             r.Host,
			Reload:           param.Reload,
			Mode:             param.getMode().String(),
			ShowBrowseButton: false,
			IsDirectoryIndex: true,
			HasReadme:        readmeErr == nil,
			DirectoryName:    filepath.Base(currentDir),
			FileTree:         generateFileTree(files, dirs, currentURLPath),
			CurrentPath:      currentURLPath,
			ParentPath:       getParentPath(currentURLPath),
		}

		err = tmpl.Execute(w, templateParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	// Show README
	templateParam := TemplateParam{
		Title:            getTitle(readme),
		Body:             mdResponse(w, readme, param),
		Host:             r.Host,
		Reload:           param.Reload,
		Mode:             param.getMode().String(),
		ShowBrowseButton: true,
		IsDirectoryIndex: false,
		HasReadme:        true,
		CurrentPath:      currentURLPath,
		ParentPath:       getParentPath(currentURLPath),
	}

	// Generate file tree for popover
	files, dirs, err := app.ListDirectoryContents(currentDir, extensions)
	if err == nil {
		templateParam.FileTree = generateFileTree(files, dirs, currentURLPath)
	}

	err = tmpl.Execute(w, templateParam)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// generateDirectoryIndex creates FileInfo slice from file paths
func generateDirectoryIndex(files []string) []FileInfo {
	fileInfos := make([]FileInfo, 0, len(files))

	for _, file := range files {
		// Calculate depth based on number of path separators
		depth := strings.Count(file, string(filepath.Separator))

		fileInfos = append(fileInfos, FileInfo{
			Name:  filepath.Base(file),
			Path:  file,
			Depth: depth,
		})
	}

	return fileInfos
}

// generateFileTree creates FileTreeItem slice from files and directories
func generateFileTree(files []string, dirs []string, currentPath string) []FileTreeItem {
	items := make([]FileTreeItem, 0, len(dirs)+len(files))

	// Add directories first
	for _, dir := range dirs {
		dirPath := filepath.Join(currentPath, dir)
		if currentPath == "" || currentPath == "." {
			dirPath = dir
		}
		items = append(items, FileTreeItem{
			Name:     dir,
			Path:     dirPath,
			IsDir:    true,
			IsBinary: false,
			Children: nil,
		})
	}

	// Add files
	for _, file := range files {
		filePath := filepath.Join(currentPath, file)
		if currentPath == "" || currentPath == "." {
			filePath = file
		}
		items = append(items, FileTreeItem{
			Name:     file,
			Path:     filePath,
			IsDir:    false,
			IsBinary: false, // Will be determined in template if needed
		})
	}

	return items
}

// getParentPath returns the parent path of the current path
func getParentPath(currentPath string) string {
	if currentPath == "" || currentPath == "." || currentPath == "/" {
		return ""
	}
	parent := filepath.Dir(currentPath)
	if parent == "." {
		return ""
	}
	return parent
}
