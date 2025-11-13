package server

import (
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/thiagokokada/gh-gfm-preview/internal/app"
	"github.com/thiagokokada/gh-gfm-preview/internal/browser"
	"github.com/thiagokokada/gh-gfm-preview/internal/utils"
)

//go:generate go run _tools/generate-assets.go

//go:embed template.html
var htmlTemplate string

//go:embed static/*
var staticDir embed.FS
var tmpl = template.Must(template.New("HTML Template").Parse(htmlTemplate))

const defaultPort = 3333

func (server *Server) Serve(param *Param) error {
	host := server.Host

	port := defaultPort
	if server.Port > 0 {
		port = server.Port
	}

	filename := ""
	dir := ""

	var err error
	if !param.UseStdin {
		// Check if filename is a directory
		inputPath := param.Filename
		if inputPath == "" {
			inputPath = "."
		}

		info, statErr := os.Stat(inputPath)
		isDir := statErr == nil && info.IsDir()

		if isDir && param.DirectoryListing {
			// Directory listing mode
			param.IsDirectoryMode = true
			param.DirectoryPath = inputPath
			dir = inputPath

			// Try to find README
			readme, readmeErr := app.FindReadme(inputPath)
			if readmeErr == nil {
				param.ReadmeFile = readme
				filename = readme
			} else {
				// No README found, will show directory listing
				filename = ""
			}
		} else {
			// Regular file mode
			filename, err = app.TargetFile(param.Filename)
			if err != nil {
				if param.DirectoryListing && errors.Is(err, app.ErrFileNotFound) {
					// README not found but directory listing is enabled
					param.IsDirectoryMode = true
					param.DirectoryPath = inputPath
					dir = inputPath
					filename = ""
				} else {
					return fmt.Errorf("target file error: %w", err)
				}
			} else {
				dir = filepath.Dir(filename)
			}
		}
	} else {
		dir = "."
	}

	serveMux := http.NewServeMux()
	serveMux.Handle("/", wrapHandler(handler(filename, param, http.FileServer(http.Dir(dir)))))
	serveMux.Handle("/static/", wrapHandler(handler(filename, param, http.FileServer(http.FS(staticDir)))))
	serveMux.Handle("/__/md", wrapHandler(mdHandler(filename, param)))

	watcher, err := createWatcher(dir)
	if err != nil {
		return err
	}
	defer watcher.Close()

	serveMux.Handle("/ws", wsHandler(watcher))

	listener, err := getTCPListener(host, port)
	if err != nil {
		return err
	}

	address := listener.Addr()

	utils.LogInfof("Accepting connections at http://%s/\n", address)

	if param.AutoOpen {
		utils.LogInfof("Open http://%s/ on your browser\n", address)

		go func() {
			err := browser.OpenBrowser(fmt.Sprintf("http://%s/", address))
			if err != nil {
				utils.LogInfof("Error while opening browser: %s\n", err)
			}
		}()
	}

	hs := &http.Server{
		Handler:      serveMux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	err = hs.Serve(listener)
	if err != nil {
		return fmt.Errorf("http server error: %w", err)
	}

	return nil
}

func handler(filename string, param *Param, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !param.IsDirectoryMode {
			// Original single-file mode
			if !strings.HasSuffix(r.URL.Path, ".md") && r.URL.Path != "/" {
				h.ServeHTTP(w, r)
				return
			}

			templateParam := TemplateParam{
				Title:  getTitle(filename),
				Body:   mdResponse(w, filename, param),
				Host:   r.Host,
				Reload: param.Reload,
				Mode:   param.getMode().String(),
			}

			err := tmpl.Execute(w, templateParam)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		// Directory mode - extract current path from URL
		urlPath := strings.TrimPrefix(r.URL.Path, "/")
		urlPath = strings.TrimSuffix(urlPath, "/")

		extensions := app.ParseExtensions(param.DirectoryListingExtensions)

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
		absBase, _ := filepath.Abs(param.DirectoryPath)
		absCurrent, _ := filepath.Abs(currentDir)
		if !strings.HasPrefix(absCurrent, absBase) {
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
	})
}

func mdResponse(w http.ResponseWriter, filename string, param *Param) string {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var markdown string

	var err error

	if param.UseStdin && param.StdinContent != "" && filename == "" {
		markdown = param.StdinContent
	} else {
		markdown, err = app.Slurp(filename)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return ""
		}
	}

	if err != nil {
		if errors.Is(err, app.ErrFileNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return ""
	}

	html, err := app.ToHTML(markdown, param.MarkdownMode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return ""
	}

	return html
}

func mdHandler(filename string, param *Param) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathParam := r.URL.Query().Get("path")

		file := cmp.Or(pathParam, filename)
		html := mdResponse(w, file, param)
		title := getTitle(file)

		body, err := json.Marshal(mdResponseJSON{HTML: html, Title: title})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		fmt.Fprintf(w, "%s", body)
	})
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func wrapHandler(wrappedHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lrw := newLoggingResponseWriter(w)
		wrappedHandler.ServeHTTP(lrw, r)

		statusCode := lrw.statusCode
		utils.LogInfof("%s [%d] %s", r.Method, statusCode, r.URL)
	})
}

func getTitle(filename string) string {
	return filepath.Base(filename)
}

func getTCPListener(host string, port int) (net.Listener, error) {
	var err error

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		utils.LogInfof("Skipping port %d: %v", port, err)
		listener, err = net.Listen("tcp", host+":0")
	}

	if err != nil {
		return nil, fmt.Errorf("TCP listener error: %w", err)
	}

	return listener, nil
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
			Name:  file,
			Path:  filePath,
			IsDir: false,
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
