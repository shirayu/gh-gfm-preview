package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandler(t *testing.T) {
	filename := "../../testdata/markdown-demo.md"
	dir := filepath.Dir(filename)
	param := &Param{
		Reload: false,
	}

	ts := httptest.NewServer(handler(filename, param, http.FileServer(http.Dir(dir))))
	defer ts.Close()

	r1, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("unexpected: %v\n", err)
	}
	defer r1.Body.Close()

	if r1.StatusCode != http.StatusOK {
		t.Errorf("server status error, got: %v", r1.StatusCode)
	}

	if r1.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("content type error, got: %s\n", r1.Header.Get("Content-Type"))
	}

	r2, err := http.Get(ts.URL + "/images/dinotocat.png")
	if err != nil {
		t.Fatalf("unexpected: %v\n", err)
	}
	defer r2.Body.Close()

	if r2.StatusCode != http.StatusOK {
		t.Errorf("server status error, got: %v", r1.StatusCode)
	}

	if r2.Header.Get("Content-Type") != "image/png" {
		t.Errorf("content type error, got: %s\n", r2.Header.Get("Content-Type"))
	}
}

func TestMdHandler(t *testing.T) {
	filename := "../../testdata/markdown-demo.md"

	ts := httptest.NewServer(mdHandler(filename, &Param{}))
	defer ts.Close()

	res, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("unexpected: %v\n", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("server status error, got: %v", res.StatusCode)
	}

	if res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("content type error, got: %s\n", res.Header.Get("Content-Type"))
	}
}

func TestWrapHandler(t *testing.T) {
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "Hello")
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lrw := newLoggingResponseWriter(w)
		wrappedHandler.ServeHTTP(lrw, r)
		statusCode := lrw.statusCode

		// XXX
		if statusCode != http.StatusOK {
			t.Errorf("logging response status code error, got: %v", statusCode)
		}
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	res, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("unexpected: %v\n", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("server status error, got: %v", res.StatusCode)
	}
}

func TestDirectoryBrowsingMode(t *testing.T) {
	testDir := "../../testdata"
	param := &Param{
		DirectoryListing:           true,
		DirectoryListingExtensions: ".md",
		IsDirectoryMode:            true,
		DirectoryPath:              testDir,
		ReadmeFile:                 filepath.Join(testDir, "README"),
		Reload:                     false,
	}

	ts := httptest.NewServer(handler("", param, http.FileServer(http.Dir(testDir))))
	defer ts.Close()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"Root README", "/", http.StatusOK},
		{"Markdown file", "/markdown-demo.md", http.StatusOK},
		{"Non-existent file", "/does-not-exist.md", http.StatusNotFound},
		{"Directory listing", "/?view=index", http.StatusOK},
		{"Subdirectory access", "/images/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatus {
				t.Errorf("status code error for %s: got %v, want %v", tt.path, res.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestGenerateFileTree(t *testing.T) {
	tests := []struct {
		name        string
		files       []string
		dirs        []string
		currentPath string
		want        []FileTreeItem
	}{
		{
			name:        "Root directory",
			files:       []string{"file1.md", "file2.md"},
			dirs:        []string{"dir1", "dir2"},
			currentPath: "",
			want: []FileTreeItem{
				{Name: "dir1", Path: "dir1", IsDir: true},
				{Name: "dir2", Path: "dir2", IsDir: true},
				{Name: "file1.md", Path: "file1.md", IsDir: false},
				{Name: "file2.md", Path: "file2.md", IsDir: false},
			},
		},
		{
			name:        "Subdirectory",
			files:       []string{"file.md"},
			dirs:        []string{"subdir"},
			currentPath: "parent",
			want: []FileTreeItem{
				{Name: "subdir", Path: "parent/subdir", IsDir: true},
				{Name: "file.md", Path: "parent/file.md", IsDir: false},
			},
		},
		{
			name:        "Nested subdirectory",
			files:       []string{},
			dirs:        []string{"BB"},
			currentPath: "AA",
			want: []FileTreeItem{
				{Name: "BB", Path: "AA/BB", IsDir: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateFileTree(tt.files, tt.dirs, tt.currentPath)
			if len(got) != len(tt.want) {
				t.Errorf("generateFileTree() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name || got[i].Path != tt.want[i].Path || got[i].IsDir != tt.want[i].IsDir {
					t.Errorf("generateFileTree()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
