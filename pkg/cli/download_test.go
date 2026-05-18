package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

type mockDownloadCommand struct {
	cid       string
	output    string
	force     bool
	dryRun    bool
	limit     int
	args      []string
}

func (m *mockDownloadCommand) Args() cli.Args {
	if m.args == nil {
		m.args = []string{m.cid}
	}
	return &mockArgs{m.args}
}

func (m *mockDownloadCommand) String(name string) string {
	switch name {
	case FlagOutput:
		return m.output
	default:
		return ""
	}
}

func (m *mockDownloadCommand) Int(name string) int {
	switch name {
	case FlagLimit:
		return m.limit
	default:
		return 0
	}
}

func (m *mockDownloadCommand) Bool(name string) bool {
	switch name {
	case FlagForce:
		return m.force
	case FlagDryRun:
		return m.dryRun
	default:
		return false
	}
}

func testDownloadConfigMgr(t *testing.T) *configmocks.MockManager {
	t.Helper()
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:          true,
		BaseEndpoint:    "pinner.xyz",
		AuthToken:       "test-token",
		MaxRetries:      3,
		GatewayEndpoint: "https://gateway.ipfs.io",
	})
	return cfgMgr
}

func TestHandleDownload_DryRun(t *testing.T) {
	tests := []struct {
		name     string
		cid      string
		output   string
		force    bool
		wantErr  bool
	}{
		{
			name:    "dry run with CID",
			cid:     "QmXxx",
			wantErr: false,
		},
		{
			name:    "dry run with output path",
			cid:     "QmXxx",
			output:  "/tmp/file.txt",
			wantErr: false,
		},
		{
			name:    "dry run with force",
			cid:     "QmXxx",
			force:   true,
			wantErr: false,
		},
		{
			name:    "dry run missing CID",
			cid:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := testDownloadConfigMgr(t)
			output := NewOutputFormatter(false, false, false, false)

			cmd := &mockDownloadCommand{
				cid:    tt.cid,
				output: tt.output,
				force:  tt.force,
				dryRun: true,
			}

			cfgMgrFactory := func() (config.Manager, error) {
				return cfgMgr, nil
			}

			downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
				return NewMockDownloadService(t)
			}

			err := handleDownload(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHandleDownload_RequiresCID(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)

	cmd := &mockDownloadCommand{cid: ""}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return NewMockDownloadService(t)
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRequired))
}

func TestHandleDownload_Success(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Download(context.Background(), "QmXxx", "", false).Return(&DownloadResult{
		CID:      "QmXxx",
		Path:     "QmXxx",
		Size:     1024,
		Duration: 100 * time.Millisecond,
	}, nil)

	cmd := &mockDownloadCommand{cid: "QmXxx"}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.NoError(t, err)
}

func TestHandleDownload_NotAuthenticated(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	cmd := &mockDownloadCommand{cid: "QmXxx"}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.Error(t, err)
}

func TestHandleDownload_FileExists_NoForce(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Download(context.Background(), "QmXxx", "", false).Return(nil, errors.New("file already exists"))

	cmd := &mockDownloadCommand{cid: "QmXxx"}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file already exists")
}

func TestHandleDownload_WithForce(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Download(context.Background(), "QmXxx", "existing.txt", true).Return(&DownloadResult{
		CID:      "QmXxx",
		Path:     "existing.txt",
		Size:     512,
		Duration: 50 * time.Millisecond,
	}, nil)

	cmd := &mockDownloadCommand{cid: "QmXxx", output: "existing.txt", force: true}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.NoError(t, err)
}

func TestHandleCat_Success(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, true, true, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Cat(context.Background(), "QmXxx").Return(io.NopCloser(strings.NewReader("hello world")), nil)

	cmd := &mockDownloadCommand{cid: "QmXxx"}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleCat(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.NoError(t, err)
}

func TestHandleCat_RequiresCID(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)

	cmd := &mockDownloadCommand{cid: ""}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return NewMockDownloadService(t)
	}

	err := handleCat(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRequired))
}

func TestHandleCat_NotAuthenticated(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	cmd := &mockDownloadCommand{cid: "QmXxx"}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleCat(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.Error(t, err)
}

func TestHandleLs_Success(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().ListDirectory(context.Background(), "QmXxx").Return([]DirEntry{
		{Name: "file1.txt", Size: 100, Type: "file"},
		{Name: "subdir", Size: -1, Type: "directory"},
	}, nil)

	cmd := &mockDownloadCommand{cid: "QmXxx", limit: 10}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleLs(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.NoError(t, err)
}

func TestHandleLs_EmptyDirectory(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().ListDirectory(context.Background(), "QmXxx").Return([]DirEntry{}, nil)

	cmd := &mockDownloadCommand{cid: "QmXxx", limit: 10}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleLs(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.NoError(t, err)
}

func TestHandleLs_RequiresCID(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)

	cmd := &mockDownloadCommand{cid: "", limit: 10}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return NewMockDownloadService(t)
	}

	err := handleLs(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRequired))
}

func TestHandleLs_WithLimit(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, false, false, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().ListDirectory(context.Background(), "QmXxx").Return([]DirEntry{
		{Name: "file1.txt", Size: 100, Type: "file"},
		{Name: "file2.txt", Size: 200, Type: "file"},
		{Name: "file3.txt", Size: 300, Type: "file"},
	}, nil)

	cmd := &mockDownloadCommand{cid: "QmXxx", limit: 2}

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleLs(context.Background(), cmd, output, cfgMgrFactory, downloadServiceFactory)
	require.NoError(t, err)
}

func TestNewDownloadCommand(t *testing.T) {
	t.Run("creates download command with correct configuration", func(t *testing.T) {
		cmd := newDownloadCommand()

		assert.Equal(t, "download", cmd.Name)
		assert.Equal(t, "<cid>[/<path>]", cmd.ArgsUsage)

		flagNames := make(map[string]bool)
		for _, flag := range cmd.Flags {
			flagNames[flag.Names()[0]] = true
		}

		assert.True(t, flagNames["output"], "should have --output flag")
		assert.True(t, flagNames["force"], "should have --force flag")
		assert.True(t, flagNames["dry-run"], "should have --dry-run flag")
	})
}

func TestNewCatCommand(t *testing.T) {
	t.Run("creates cat command with correct configuration", func(t *testing.T) {
		cmd := newCatCommand()

		assert.Equal(t, "cat", cmd.Name)
		assert.Equal(t, "<cid>[/<path>]", cmd.ArgsUsage)
		assert.Empty(t, cmd.Flags, "cat should have no command-specific flags")
		assert.NotNil(t, cmd.Action)
	})
}

func TestNewLsCommand(t *testing.T) {
	t.Run("creates ls command with correct configuration", func(t *testing.T) {
		cmd := newLsCommand()

		assert.Equal(t, "ls", cmd.Name)
		assert.Equal(t, "<cid>[/<path>]", cmd.ArgsUsage)

		flagNames := make(map[string]bool)
		for _, flag := range cmd.Flags {
			flagNames[flag.Names()[0]] = true
		}

		assert.True(t, flagNames["limit"], "should have --limit flag")
	})
}

func TestDefaultDownloadServiceFactory(t *testing.T) {
	t.Run("creates download service with correct dependencies", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			BaseEndpoint: "https://api.test.com",
			AuthToken:    "test-token",
			Secure:       true,
		})

		output := NewOutputFormatter(false, false, false, false)

		service := defaultDownloadServiceFactory(cfgMgr, output)

		assert.IsType(t, &DownloadServiceDefault{}, service)
		ds := service.(*DownloadServiceDefault)
		assert.Equal(t, cfgMgr, ds.configMgr)
		assert.Equal(t, output, ds.output)
		assert.Equal(t, "https://ipfs.api.test.com", ds.ipfsEndpoint)
	})
}
