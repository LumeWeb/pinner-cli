package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

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
			output := newTestOutput()

			cmd := newMockCommand().
				withArgs(tt.cid).
				withString(FlagOutput, tt.output).
				withBool(FlagForce, tt.force).
				withBool(FlagDryRun, true)


			downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
				return NewMockDownloadService(t)
			}

			err := handleDownload(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))

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
	output := newTestOutput()

	cmd := newMockCommand()


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return NewMockDownloadService(t)
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRequired))
}

func TestHandleDownload_Success(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Download(mock.Anything, "QmXxx", "", false).Return(&DownloadResult{
		CID:      "QmXxx",
		Path:     "QmXxx",
		Size:     1024,
		Duration: 100 * time.Millisecond,
	}, nil)

	cmd := newMockCommand().withArgs("QmXxx")


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.NoError(t, err)
}

func TestHandleDownload_NotAuthenticated(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	cmd := newMockCommand().withArgs("QmXxx")


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.Error(t, err)
}

func TestHandleDownload_FileExists_NoForce(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Download(mock.Anything, "QmXxx", "", false).Return(nil, errors.New("file already exists"))

	cmd := newMockCommand().withArgs("QmXxx")


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file already exists")
}

func TestHandleDownload_WithForce(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Download(mock.Anything, "QmXxx", "existing.txt", true).Return(&DownloadResult{
		CID:      "QmXxx",
		Path:     "existing.txt",
		Size:     512,
		Duration: 50 * time.Millisecond,
	}, nil)

	cmd := newMockCommand().
		withArgs("QmXxx").
		withString(FlagOutput, "existing.txt").
		withBool(FlagForce, true)


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleDownload(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.NoError(t, err)
}

func TestHandleCat_Success(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := NewOutputFormatter(false, true, true, false)
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Cat(mock.Anything, "QmXxx").Return(io.NopCloser(strings.NewReader("hello world")), nil)

	cmd := newMockCommand().withArgs("QmXxx")


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleCat(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.NoError(t, err)
}

func TestHandleCat_RequiresCID(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()

	cmd := newMockCommand()


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return NewMockDownloadService(t)
	}

	err := handleCat(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRequired))
}

func TestHandleCat_NotAuthenticated(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	cmd := newMockCommand().withArgs("QmXxx")


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleCat(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.Error(t, err)
}

func TestHandleLs_Success(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().ListDirectory(mock.Anything, "QmXxx").Return([]DirEntry{
		{Name: "file1.txt", Size: 100, Type: "file"},
		{Name: "subdir", Size: -1, Type: "directory"},
	}, nil)

	cmd := newMockCommand().withArgs("QmXxx").withInt(FlagLimit, 10)


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleLs(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.NoError(t, err)
}

func TestHandleLs_EmptyDirectory(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().ListDirectory(mock.Anything, "QmXxx").Return([]DirEntry{}, nil)

	cmd := newMockCommand().withArgs("QmXxx").withInt(FlagLimit, 10)


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleLs(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.NoError(t, err)
}

func TestHandleLs_RequiresCID(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()

	cmd := newMockCommand().withInt(FlagLimit, 10)


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return NewMockDownloadService(t)
	}

	err := handleLs(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRequired))
}

func TestHandleLs_WithLimit(t *testing.T) {
	cfgMgr := testDownloadConfigMgr(t)
	output := newTestOutput()
	service := NewMockDownloadService(t)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().ListDirectory(mock.Anything, "QmXxx").Return([]DirEntry{
		{Name: "file1.txt", Size: 100, Type: "file"},
		{Name: "file2.txt", Size: 200, Type: "file"},
		{Name: "file3.txt", Size: 300, Type: "file"},
	}, nil)

	cmd := newMockCommand().withArgs("QmXxx").withInt(FlagLimit, 2)


	downloadServiceFactory := func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
		return service
	}

	err := handleLs(context.Background(), cmd, output, cfgMgr, "test-token", true, DownloadServiceFactory(downloadServiceFactory))
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

		output := newTestOutput()

		service := defaultDownloadServiceFactory(cfgMgr, output)

		assert.IsType(t, &DownloadServiceDefault{}, service)
		ds := service.(*DownloadServiceDefault)
		assert.Equal(t, cfgMgr, ds.configMgr)
		assert.Equal(t, output, ds.output)
		assert.Equal(t, "https://ipfs.api.test.com", ds.ipfsEndpoint)
	})
}
