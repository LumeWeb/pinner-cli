package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/pinner-cli/pkg/internal/io"
)

func TestResolveUploadInput_Stdin(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		input    string
		wantName string
		wantErr  bool
	}{
		{
			name:     "stdin with default name",
			path:     "",
			input:    "hello world",
			wantName: "stdin",
			wantErr:  false,
		},
		{
			name:     "stdin with custom name",
			path:     "",
			input:    "test data",
			wantName: "custom-name",
			wantErr:  false,
		},
		{
			name:     "stdin empty error",
			path:     "",
			input:    "",
			wantName: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock stdin
			oldStdin := os.Stdin
			r, w, _ := os.Pipe()
			os.Stdin = r

			go func() {
				w.WriteString(tt.input)
				w.Close()
			}()

			defer func() {
				r.Close()
				os.Stdin = oldStdin
			}()

			input, err := resolveUploadInput(tt.path, tt.wantName)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveUploadInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if input.Name != tt.wantName {
					t.Errorf("resolveUploadInput() name = %v, want %v", input.Name, tt.wantName)
				}
				if input.Filesystem == nil {
					t.Error("resolveUploadInput() filesystem is nil")
				}
			}
		})
	}
}

type mockUploadCommand struct {
	path   string
	name   string
	wait   bool
	dryRun bool
	args   []string
}

func (m *mockUploadCommand) Args() cli.Args {
	if m.args == nil {
		m.args = []string{m.path}
	}
	return &mockArgs{m.args}
}

func (m *mockUploadCommand) Uint64(name string) uint64 {
	if name == FlagMemoryLimit {
		return 100
	}
	return 0
}

func (m *mockUploadCommand) Int64(name string) int64 {
	return 0
}

func (m *mockUploadCommand) Int(name string) int {
	return 0
}

func (m *mockUploadCommand) String(name string) string {
	switch name {
	case FlagName:
		return m.name
	default:
		return ""
	}
}

func (m *mockUploadCommand) Bool(name string) bool {
	switch name {
	case FlagWait:
		return m.wait
	case FlagDryRun:
		return m.dryRun
	case FlagSecure:
		return true
	default:
		return false
	}
}

func (m *mockUploadCommand) IsSet(name string) bool {
	return false
}

func TestResolveUploadInput_File(t *testing.T) {
	// Create a temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("test content")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		nameArg  string
		wantName string
		wantErr  bool
	}{
		{
			name:     "file with default name",
			path:     testFile,
			nameArg:  "",
			wantName: "test.txt",
			wantErr:  false,
		},
		{
			name:     "file with custom name",
			path:     testFile,
			nameArg:  "custom.txt",
			wantName: "custom.txt",
			wantErr:  false,
		},
		{
			name:     "non-existent file",
			path:     "/nonexistent/file.txt",
			nameArg:  "",
			wantName: "",
			wantErr:  true,
		},
		{
			name:     "empty path",
			path:     "",
			nameArg:  "",
			wantName: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := resolveUploadInput(tt.path, tt.nameArg)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveUploadInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if input.Name != tt.wantName {
					t.Errorf("resolveUploadInput() name = %v, want %v", input.Name, tt.wantName)
				}
				if input.Filesystem == nil {
					t.Error("resolveUploadInput() filesystem is nil")
				}
			}
		})
	}
}

func TestResolveUploadInput_Directory(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		nameArg  string
		wantName string
		wantErr  bool
	}{
		{
			name:     "directory",
			path:     tmpDir,
			nameArg:  "",
			wantName: "",
			wantErr:  false,
		},
		{
			name:     "directory with custom name",
			path:     tmpDir,
			nameArg:  "mydir",
			wantName: "mydir",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := resolveUploadInput(tt.path, tt.nameArg)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveUploadInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if input.Name != tt.wantName {
					t.Errorf("resolveUploadInput() name = %v, want %v", input.Name, tt.wantName)
				}
				if input.Filesystem == nil {
					t.Error("resolveUploadInput() filesystem is nil")
				}
			}
		})
	}
}

func TestStdinFS(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{
			name:    "valid content",
			content: []byte("test data"),
			wantErr: false,
		},
		{
			name:    "large content",
			content: bytes.Repeat([]byte("x"), 1024*1024),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock stdin
			oldStdin := os.Stdin
			r, w, _ := os.Pipe()
			os.Stdin = r

			go func() {
				w.Write(tt.content)
				w.Close()
			}()

			defer func() {
				r.Close()
				os.Stdin = oldStdin
			}()

			filesystem, err := io.NewStdinFS("testfile")
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStdinFS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Test reading the file
				file, err := filesystem.Open("testfile")
				if err != nil {
					t.Fatalf("failed to open file: %v", err)
				}
				defer file.Close()

				content, err := fs.ReadFile(filesystem, "testfile")
				if err != nil {
					t.Fatalf("failed to read file: %v", err)
				}

				if !bytes.Equal(content, tt.content) {
					t.Errorf("file content mismatch, got %d bytes, want %d bytes", len(content), len(tt.content))
				}
			}
		})
	}
}

func TestStdinFS_ReadDir(t *testing.T) {
	testContent := []byte("test data")

	// Mock stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r

	go func() {
		w.Write(testContent)
		w.Close()
	}()

	defer func() {
		r.Close()
		os.Stdin = oldStdin
	}()

	filesystem, err := io.NewStdinFS("myfile.txt")
	if err != nil {
		t.Fatalf("NewStdinFS() error = %v", err)
	}

	// Test ReadDir
	entries, err := fs.ReadDir(filesystem, ".")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("ReadDir() returned %d entries, want 1", len(entries))
	}

	if entries[0].Name() != "myfile.txt" {
		t.Errorf("entry name = %v, want myfile.txt", entries[0].Name())
	}
}

func TestStdinFS_Stat(t *testing.T) {
	testContent := []byte("test data")

	// Mock stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r

	go func() {
		w.Write(testContent)
		w.Close()
	}()

	defer func() {
		r.Close()
		os.Stdin = oldStdin
	}()

	filesystem, err := io.NewStdinFS("testfile")
	if err != nil {
		t.Fatalf("NewStdinFS() error = %v", err)
	}

	// Test statting root
	rootInfo, err := filesystem.Stat(".")
	if err != nil {
		t.Fatalf("Stat('.') error = %v", err)
	}

	if !rootInfo.IsDir() {
		t.Error("root should be a directory")
	}

	// Test statting file
	fileInfo, err := filesystem.Stat("testfile")
	if err != nil {
		t.Fatalf("Stat('testfile') error = %v", err)
	}

	if fileInfo.IsDir() {
		t.Error("file should not be a directory")
	}

	if fileInfo.Size() != int64(len(testContent)) {
		t.Errorf("file size = %v, want %v", fileInfo.Size(), len(testContent))
	}
}

func TestUploadDryRun(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		dryRunFlag  bool
		setupMocks  func(*MockUploadService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "dry run with file",
			path:       "test.txt",
			dryRunFlag: true,
			setupMocks: func(service *MockUploadService) {
			},
			wantErr: false,
		},
		{
			name:       "dry run with custom name",
			path:       "test.txt",
			dryRunFlag: true,
			setupMocks: func(service *MockUploadService) {
			},
			wantErr: false,
		},
		{
			name:       "dry run with wait flag",
			path:       "test.txt",
			dryRunFlag: true,
			setupMocks: func(service *MockUploadService) {
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temp file
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.txt")
			testContent := []byte("test content")
			if err := os.WriteFile(testFile, testContent, 0644); err != nil {
				t.Fatal(err)
			}

			service := NewMockUploadService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(service)
			}

			cmd := &mockUploadCommand{
				path:   filepath.Join(tmpDir, tt.path),
				name:   "",
				wait:   false,
				dryRun: tt.dryRunFlag,
			}

			if tt.name == "dry run with custom name" {
				cmd.name = "custom-name"
			}

			if tt.name == "dry run with wait flag" {
				cmd.wait = true
			}

			cfgMgrFactory := func() (config.Manager, error) {
				cfgMgr := configmocks.NewMockManager(t)
				cfgMgr.EXPECT().Config().Return(&config.Config{
					MemoryLimit:     100,
					Secure:          true,
					BaseEndpoint:    "pinner.xyz",
					AuthToken:       testAuthToken,
					MaxRetries:      3,
					GatewayEndpoint: "https://gateway.ipfs.io",
				})
				return cfgMgr, nil
			}

			uploadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...UploadServiceOption) UploadService {
				return service
			}

			err := handleUpload(context.Background(), cmd, output, cfgMgrFactory, uploadServiceFactory)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
