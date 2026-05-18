package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipfs/boxo/files"
	"github.com/ipfs/go-cid"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
)

type DownloadServiceDefault struct {
	accountClient   portalsdk.AccountAPI
	authService     AuthService
	configMgr       config.Manager
	output          Output
	ipfsEndpoint    string
	accountEndpoint string
	authToken       string
}

func NewDownloadService(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
	cfg := cfgMgr.Config()
	s := &DownloadServiceDefault{
		accountClient:   portalsdk.NewClient(portalsdk.WithEndpoint(cfg.GetAPIEndpoint())),
		accountEndpoint: cfg.GetAPIEndpoint(),
		ipfsEndpoint:    cfg.GetIPFSEndpointSecure(),
		configMgr:       cfgMgr,
		output:          output,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *DownloadServiceDefault) RequireAuthenticated() error {
	authToken := s.getAuthToken()
	if authToken == "" {
		return fmt.Errorf("not authenticated: please run 'pinner auth login' first or provide --auth-token")
	}
	return nil
}

func (s *DownloadServiceDefault) getAuthToken() string {
	if s.authToken != "" {
		return s.authToken
	}
	return s.configMgr.Config().AuthToken
}

func (s *DownloadServiceDefault) resolveAuthToken(ctx context.Context) (string, error) {
	authToken := s.getAuthToken()

	if s.authService != nil {
		purpose, err := GetJWTPurpose(authToken)
		if err != nil {
			s.output.PrintVerbosef("Could not decode JWT purpose, using raw token: %v", err)
			return authToken, nil
		}
		if purpose == "api" {
			s.output.PrintVerbose("Detected API key JWT, exchanging for login token for download")
			loginJWT, err := s.accountClient.LoginWithAPIKey(ctx, authToken)
			if err != nil {
				return "", fmt.Errorf("failed to exchange API key for download: %w", err)
			}
			return loginJWT, nil
		}
	}

	return authToken, nil
}

func (s *DownloadServiceDefault) newSDKDownloadService(ctx context.Context) (*ipfs.DownloadService, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	authToken, err := s.resolveAuthToken(ctx)
	if err != nil {
		return nil, err
	}

	dlService, err := ipfs.NewDownloadService(s.ipfsEndpoint, authToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create download service: %w", err)
	}

	return dlService, nil
}

type ipfsPath struct {
	cid  cid.Cid
	path string
}

func parseIPFSPath(ipfsPathStr string) (ipfsPath, error) {
	parts := strings.SplitN(ipfsPathStr, "/", 2)
	parsedCID, err := cid.Decode(parts[0])
	if err != nil {
		return ipfsPath{}, fmt.Errorf("%w: %s", ErrInvalidCID, err)
	}

	p := ipfsPath{cid: parsedCID}
	if len(parts) > 1 {
		p.path = strings.Trim(parts[1], "/")
	}

	return p, nil
}

func (s *DownloadServiceDefault) Cat(ctx context.Context, ipfsPathStr string) (io.ReadCloser, error) {
	s.output.PrintVerbosef("Using IPFS endpoint: %s", s.ipfsEndpoint)

	p, err := parseIPFSPath(ipfsPathStr)
	if err != nil {
		return nil, err
	}

	dlService, err := s.newSDKDownloadService(ctx)
	if err != nil {
		return nil, err
	}

	if p.path != "" {
		reader, err := dlService.GetFile(ctx, p.cid, p.path)
		if err != nil {
			return nil, s.wrapDownloadError(err)
		}
		return reader, nil
	}

	reader, err := dlService.DownloadFile(ctx, p.cid)
	if err != nil {
		return nil, s.wrapDownloadError(err)
	}

	return reader, nil
}

func (s *DownloadServiceDefault) Download(ctx context.Context, ipfsPathStr string, outputPath string, force bool) (*DownloadResult, error) {
	startTime := time.Now()

	s.output.PrintVerbosef("Using IPFS endpoint: %s", s.ipfsEndpoint)

	p, err := parseIPFSPath(ipfsPathStr)
	if err != nil {
		return nil, err
	}

	dlService, err := s.newSDKDownloadService(ctx)
	if err != nil {
		return nil, err
	}

	if outputPath == "" {
		if p.path != "" {
			outputPath = filepath.Base(p.path)
		} else {
			outputPath = p.cid.String()
		}
	}

	fi, err := os.Stat(outputPath)
	if err == nil && fi.IsDir() {
		if p.path != "" {
			outputPath = filepath.Join(outputPath, filepath.Base(p.path))
		} else {
			outputPath = filepath.Join(outputPath, p.cid.String())
		}
		_, statErr := os.Stat(outputPath)
		if statErr == nil && !force {
			return nil, fmt.Errorf("file already exists: %s (use --force to overwrite)", outputPath)
		}
	} else if err == nil && !force {
		return nil, fmt.Errorf("file already exists: %s (use --force to overwrite)", outputPath)
	}

	fileSize, sizeErr := dlService.FileSize(ctx, p.cid)
	if sizeErr == nil && fileSize > 0 {
		s.output.PrintVerbosef("File size: %s", humanReadableSize(fileSize))
	}

	s.output.PrintVerbosef("Downloading to: %s", outputPath)

	var reader io.ReadCloser
	if p.path != "" {
		reader, err = dlService.GetFile(ctx, p.cid, p.path)
	} else {
		reader, err = dlService.DownloadFile(ctx, p.cid)
	}
	if err != nil {
		return nil, s.wrapDownloadError(err)
	}
	defer func() { _ = reader.Close() }()

	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}

	var written int64
	written, err = io.Copy(f, reader)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(outputPath)
		return nil, fmt.Errorf("failed to write file: %w", err)
	}
	_ = f.Close()

	duration := time.Since(startTime)

	return &DownloadResult{
		CID:      p.cid.String(),
		Path:     outputPath,
		Size:     written,
		Duration: duration,
	}, nil
}

func (s *DownloadServiceDefault) FileSize(ctx context.Context, ipfsPathStr string) (int64, error) {
	p, err := parseIPFSPath(ipfsPathStr)
	if err != nil {
		return -1, err
	}

	dlService, err := s.newSDKDownloadService(ctx)
	if err != nil {
		return -1, err
	}

	size, err := dlService.FileSize(ctx, p.cid)
	if err != nil {
		return -1, s.wrapDownloadError(err)
	}

	return size, nil
}

func (s *DownloadServiceDefault) ListDirectory(ctx context.Context, ipfsPathStr string) ([]DirEntry, error) {
	s.output.PrintVerbosef("Using IPFS endpoint: %s", s.ipfsEndpoint)

	p, err := parseIPFSPath(ipfsPathStr)
	if err != nil {
		return nil, err
	}

	dlService, err := s.newSDKDownloadService(ctx)
	if err != nil {
		return nil, err
	}

	var entries []files.DirEntry
	if p.path != "" {
		entries, err = dlService.ListDirectoryPath(ctx, p.cid, p.path)
		if err != nil && isNotDirectoryError(err) {
			return s.listFileEntry(ctx, dlService, p)
		}
	} else {
		entries, err = dlService.ListDirectory(ctx, p.cid)
		if err != nil && isNotDirectoryError(err) {
			return s.listFileEntry(ctx, dlService, p)
		}
	}
	if err != nil {
		return nil, s.wrapDownloadError(err)
	}

	result := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		entryType := "file"
		node := entry.Node()
		if _, ok := node.(files.Directory); ok {
			entryType = "directory"
		}

		var size int64 = -1
		if f, ok := node.(files.File); ok {
			size, _ = f.Size()
		}

		result = append(result, DirEntry{
			Name: entry.Name(),
			Size: size,
			Type: entryType,
		})
	}

	return result, nil
}

func (s *DownloadServiceDefault) listFileEntry(ctx context.Context, dlService *ipfs.DownloadService, p ipfsPath) ([]DirEntry, error) {
	name := p.cid.String()
	if p.path != "" {
		name = filepath.Base(p.path)
	}

	var fileSize int64 = -1
	if p.path != "" {
		reader, err := dlService.GetFile(ctx, p.cid, p.path)
		if err == nil {
			if seeker, ok := reader.(io.Seeker); ok {
				end, _ := seeker.Seek(0, io.SeekEnd)
				if end > 0 {
					fileSize = end
				}
				_, _ = seeker.Seek(0, io.SeekStart)
			}
			_ = reader.Close()
		}
	} else {
		fs, err := dlService.FileSize(ctx, p.cid)
		if err == nil {
			fileSize = fs
		}
	}

	return []DirEntry{
		{
			Name: name,
			Size: fileSize,
			Type: "file",
		},
	}, nil
}

func isNotDirectoryError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "path is not a directory" || msg == "CID is not a directory"
}

func (s *DownloadServiceDefault) wrapDownloadError(err error) error {
	if err == nil {
		return nil
	}
	return WrapAuthError("Download", err)
}
