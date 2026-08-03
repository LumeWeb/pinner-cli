package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

// createVaultDownloadTemp creates a uniquely-named temp file in dir for a
// vault download. It opens with O_CREATE|O_EXCL and mode 0666 so the kernel
// applies the process umask atomically at open (as os.Create would), honoring
// a restrictive umask (e.g. 077) with no syscall.Umask mutation, no TOCTOU
// race, and no post-hoc chmod. O_EXCL plus a random name prevents symlink
// following and reuse of a stale temp from a crashed prior run.
func createVaultDownloadTemp(dir string) (*os.File, error) {
	for i := 0; i < 10000; i++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, ".vault-download-"+hex.EncodeToString(b[:])+".tmp")
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("failed to create unique temp file in %s", dir)
}

func newVaultCpCommand() *cli.Command {
	return &cli.Command{
		Name:      "cp",
		Usage:     "Copy files between local filesystem and vault",
		ArgsUsage: "<src> <dst>",
		Description: `Copy files in either direction:

  Upload:   pinner vault cp ./local.txt vault:/docs/local.txt
  Download: pinner vault cp vault:/docs/local.txt ./

One argument must be a vault:/ path and the other a local path.
Files are never overwritten without --force.`,
		Flags: []cli.Flag{
			ForceFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			args := c.Args()
			if args.Len() < 2 {
				return fmt.Errorf("usage: pinner vault cp <src> <dst>")
			}
			src := args.Get(0)
			dst := args.Get(1)

			srcIsVault := vault.IsVaultPath(src)
			dstIsVault := vault.IsVaultPath(dst)

			if srcIsVault && !dstIsVault {
				return vaultDownload(ctx, c, output, src, dst)
			} else if !srcIsVault && dstIsVault {
				return vaultUpload(ctx, c, output, src, dst)
			} else {
				return fmt.Errorf("one argument must be a vault:/ path and the other a local path")
			}
		},
	}
}

func vaultUpload(ctx context.Context, c *cli.Command, output Output, localPath, vaultPath string) error {
	// Expand directory destinations: vault:/docs/ → vault:/docs/<filename>
	if strings.HasSuffix(vaultPath, "/") {
		vaultPath = vaultPath + filepath.Base(localPath)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}

	svc, _, err := vaultServiceForCommand(c)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Check if destination already exists (unless --force). Only a NotFound
	// result means the path is free to write; any other Stat error (e.g. a
	// transient indexer or config failure) must abort rather than fall through
	// to Put, which would delete a prior record/object without --force.
	if !c.Bool(FlagForce) {
		if _, err := svc.Stat(ctx, vaultPath); err == nil {
			return fmt.Errorf("file already exists in vault: %s (use --force to overwrite)", vaultPath)
		} else if !errors.Is(err, vault.ErrNotFound) {
			return fmt.Errorf("cannot check destination %s: %w", vaultPath, err)
		}
	}

	// Wrap reader with progress (stderr — stdout stays clean for piping)
	var reader io.Reader = f
	if !output.IsJSON() {
		pr := newProgressReader(f, stat.Size(), "Uploading")
		defer pr.Close()
		reader = pr
	}

	record, err := svc.Put(ctx, reader, stat.Size(), vaultPath, nil)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		output.PrintJSON(vaultCpResponse{
			Path:          vaultPath,
			ObjectID:      record.ObjectKey,
			Size:          record.Size,
			ContentDigest: record.ContentDigest,
		})
	} else {
		fmt.Println(vaultPath)
		output.Printfln("Uploaded %d bytes → %s", record.Size, vaultPath)
	}
	return nil
}

func vaultDownload(ctx context.Context, c *cli.Command, output Output, vaultPath, localPath string) error {
	// Expand directory destinations: ./ → ./<filename from vault>
	if localPath == "." || strings.HasSuffix(localPath, "/") {
		vp, err := vault.ParseVaultPath(vaultPath)
		if err != nil {
			return fmt.Errorf("invalid vault path: %w", err)
		}
		localPath = filepath.Join(localPath, vp.Name)
	}

	// Check if file exists, handle --force
	if _, err := os.Stat(localPath); err == nil && !c.Bool(FlagForce) {
		return fmt.Errorf("file exists: %s (use --force to overwrite)", localPath)
	}

	// Download to a temp file in the same directory, then atomically rename
	// onto localPath only after the download succeeds. This prevents --force
	// from silently truncating an existing file when the vault service can't
	// be created or the download fails partway.
	//
	// The temp file is opened with O_CREATE|O_EXCL and mode 0666. O_EXCL
	// plus a random name prevents symlink following and stale-tmp reuse, and
	// the kernel applies the process umask atomically at open (exactly as
	// os.Create would), so a restrictive umask such as 077 is honored — no
	// syscall.Umask mutation, no TOCTOU race, no post-hoc chmod.
	f, err := createVaultDownloadTemp(filepath.Dir(localPath))
	if err != nil {
		return err
	}
	tmp := f.Name()

	svc, _, err := vaultServiceForCommand(c)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	defer svc.Close()

	// Wrap writer with progress (stderr — stdout stays clean for piping)
	var writer io.Writer = f
	if !output.IsJSON() {
		pw := newProgressWriter(f, 0, "Downloading")
		defer pw.Close()
		writer = pw
	}

	if err := svc.Get(ctx, vaultPath, writer); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Atomically move the temp file onto the destination. The cross-platform
	// helper performs an atomic replace (on Windows it maps to MoveFileEx with
	// MOVEFILE_REPLACE_EXISTING), so a failed overwrite leaves any existing
	// destination intact rather than deleting it first and losing the original
	// if the move then fails.
	if err := replaceDownloadedFile(tmp, localPath); err != nil {
		os.Remove(tmp)
		return err
	}
	output.Printfln("Downloaded → %s", localPath)
	return nil
}
