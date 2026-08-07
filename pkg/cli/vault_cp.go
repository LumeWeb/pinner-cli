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
// vault download/copy, opening with O_CREATE|O_EXCL and the given mode so the
// kernel applies the process umask atomically at open (as os.Create would),
// honoring a restrictive umask (e.g. 077) with no syscall.Umask mutation, no
// TOCTOU race, and no post-hoc chmod. O_EXCL plus a random name prevents
// symlink following and reuse of a stale temp from a crashed prior run.
//
// The caller chooses the mode: the download path passes 0o666 (umask-honoring,
// typically 0644) because the temp is atomically renamed onto the destination,
// so the final file inherits it. The vault↔vault copy path passes 0o600 because
// it buffers decrypted plaintext and must never be world-readable even under a
// permissive umask.
func createVaultDownloadTemp(dir string, mode os.FileMode) (*os.File, error) {
	for i := 0; i < 10000; i++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, ".vault-download-"+hex.EncodeToString(b[:])+".tmp")
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("failed to create unique temp file in %s", dir)
}

// cpEndpoint classifies a single `vault cp` argument.
type cpEndpoint struct {
	isVault   bool
	profile   string // for vault: named profile authority, or "" = active profile
	vaultPath string // for vault: ScalarPath (authority-stripped, service-operable)
	name      string // for vault: leaf file/dir name
	localPath string // for local: the filesystem path
	raw       string // original argument (for messages)
	isDir     bool   // for vault: true if the raw path ends with "/"
}

// classifyCpArg parses one cp argument into a local or vault endpoint. Vault
// paths honor an explicit "vault://<profile>/" authority; the profile is
// captured separately from the service-operable path.
func classifyCpArg(arg string) *cpEndpoint {
	if !vault.IsVaultPath(arg) {
		return &cpEndpoint{isVault: false, localPath: arg, raw: arg}
	}
	vp, err := vault.ParseVaultPath(arg)
	if err != nil {
		// Non-vault fallback (should not happen for an IsVaultPath prefix).
		return &cpEndpoint{isVault: false, localPath: arg, raw: arg}
	}
	profile := ""
	if vp.Profile != nil {
		profile = *vp.Profile
	}
	return &cpEndpoint{
		isVault:   true,
		profile:   profile,
		vaultPath: vp.ScalarPath(),
		name:      vp.Name,
		localPath: "",
		raw:       arg,
		isDir:     vp.IsDir,
	}
}

// resolveService builds the VaultService for a cp vault endpoint's profile
// ("" = active profile, resolved from the command).
func resolveService(c *cli.Command, ep *cpEndpoint) (vault.VaultService, error) {
	if ep.profile == "" {
		svc, _, err := vaultServiceForCommand(c)
		return svc, err
	}
	if err := vault.ValidateProfileName(ep.profile); err != nil {
		return nil, fmt.Errorf("invalid profile in %q: %w", ep.raw, err)
	}
	return newVaultService(ep.profile)
}

func newVaultCpCommand() *cli.Command {
	return &cli.Command{
		Name:      "cp",
		Usage:     "Copy files between the local filesystem and vault (and vault to vault)",
		ArgsUsage: "<src> <dst>",
		Description: `Copy a file between any two of: the local filesystem, the active
vault, or a named profile's vault.

  Upload:        pinner vault cp ./local.txt vault:/docs/local.txt
  Download:      pinner vault cp vault:/docs/local.txt ./
  Named vault → local: pinner vault cp vault://work/docs/a.txt ./
  Vault → vault:     pinner vault cp vault://work/docs/a.txt vault:/docs/a.txt

A vault:/ path uses the active profile; vault://<profile>/ names a specific
profile. The destination must not exist without --force to overwrite.
Directory-tree copy is not yet supported (single file only).`,
		Flags: []cli.Flag{
			ForceFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			args := c.Args()
			if args.Len() < 2 {
				return fmt.Errorf("usage: pinner vault cp <src> <dst>")
			}
			src := classifyCpArg(args.Get(0))
			dst := classifyCpArg(args.Get(1))

			switch {
			case !src.isVault && !dst.isVault:
				return fmt.Errorf("both arguments are local paths; use cp(1) to copy local files")
			case !src.isVault && dst.isVault:
				return vaultUpload(ctx, c, output, src, dst)
			case src.isVault && !dst.isVault:
				return vaultDownload(ctx, c, output, src, dst)
			default:
				return vaultVaultCopy(ctx, c, output, src, dst)
			}
		},
	}
}

func vaultUpload(ctx context.Context, c *cli.Command, output Output, localEp, vaultEp *cpEndpoint) error {
	localPath := localEp.localPath

	// The destination vault path, expanded if it is a directory destination.
	vaultPath := vaultEp.vaultPath
	if vaultEp.isDir {
		vaultPath = vault.JoinVaultPath(vaultPath, filepath.Base(localPath))
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

	svc, err := resolveService(c, vaultEp)
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
			return fmt.Errorf("file already exists in vault: %s (use --force to overwrite)", vaultEp.raw)
		} else if !errors.Is(err, vault.ErrNotFound) {
			return fmt.Errorf("cannot check destination %s: %w", vaultEp.raw, err)
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
			Path:          vaultEp.raw,
			ObjectID:      record.ObjectKey,
			Size:          record.Size,
			ContentDigest: record.ContentDigest,
		})
	} else {
		fmt.Println(vaultEp.raw)
		output.Printfln("Uploaded %d bytes → %s", record.Size, vaultEp.raw)
	}
	return nil
}

func vaultDownload(ctx context.Context, c *cli.Command, output Output, vaultEp, localEp *cpEndpoint) error {
	localPath := localEp.localPath

	// Expand directory destinations: ./ → ./<filename from vault>, and a
	// plain existing directory → <dir>/<filename from vault>.
	if localPath == "." || strings.HasSuffix(localPath, "/") {
		localPath = filepath.Join(localPath, vaultEp.name)
	} else if fi, err := os.Stat(localPath); err == nil && fi.IsDir() {
		localPath = filepath.Join(localPath, vaultEp.name)
	}

	// Check if file exists, handle --force
	if _, err := os.Stat(localPath); err == nil && !c.Bool(FlagForce) {
		return fmt.Errorf("file exists: %s (use --force to overwrite)", localPath)
	}

	// Download to a temp file in the same directory, then atomically rename
	// onto localPath only after the download succeeds, so --force never
	// truncates an existing file when the service can't be built or the
	// download fails partway. The temp is created at 0666 (umask-honoring,
	// typically 0644) because the rename preserves its mode as the final
	// destination file's mode.
	f, err := createVaultDownloadTemp(filepath.Dir(localPath), 0o666)
	if err != nil {
		return err
	}
	tmp := f.Name()

	svc, err := resolveService(c, vaultEp)
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

	if err := svc.Get(ctx, vaultEp.vaultPath, writer); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Atomically move the temp file onto the destination.
	if err := replaceDownloadedFile(tmp, localPath); err != nil {
		os.Remove(tmp)
		return err
	}
	output.Printfln("Downloaded → %s", localPath)
	return nil
}

// vaultVaultCopy streams a file between two vaults (which may be different
// profiles). The source is downloaded into a temp file, then uploaded to the
// destination path — so files of any size copy without buffering in memory.
func vaultVaultCopy(ctx context.Context, c *cli.Command, output Output, srcEp, dstEp *cpEndpoint) error {
	// Destination may be a directory path; expand with the source filename.
	dstPath := dstEp.vaultPath
	if dstEp.isDir {
		dstPath = vault.JoinVaultPath(dstPath, srcEp.name)
	}

	srcSvc, err := resolveService(c, srcEp)
	if err != nil {
		return err
	}
	defer srcSvc.Close()
	dstSvc, err := resolveService(c, dstEp)
	if err != nil {
		return err
	}
	defer dstSvc.Close()

	// Buffer the source in a temp file inside a private directory so the
	// Get→Put copy can stream without holding the whole object in memory and
	// without ever exposing decrypted plaintext to other local users. The
	// directory is created with mode 0700 and removed (with the file) on exit;
	// the file itself is additionally opened at 0600.
	tmpDir, err := os.MkdirTemp("", "vault-cp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	f, err := createVaultDownloadTemp(tmpDir, 0o600)
	if err != nil {
		return err
	}
	tmp := f.Name()

	if err := srcSvc.Get(ctx, srcEp.vaultPath, f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	stat, err := os.Stat(tmp)
	if err != nil {
		return err
	}

	// Check destination (unless --force), mirroring upload.
	if !c.Bool(FlagForce) {
		if _, err := dstSvc.Stat(ctx, dstPath); err == nil {
			return fmt.Errorf("file already exists in vault: %s (use --force to overwrite)", dstEp.raw)
		} else if !errors.Is(err, vault.ErrNotFound) {
			return fmt.Errorf("cannot check destination %s: %w", dstEp.raw, err)
		}
	}

	up, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer up.Close()

	var reader io.Reader = up
	if !output.IsJSON() {
		pr := newProgressReader(up, stat.Size(), "Copying")
		defer pr.Close()
		reader = pr
	}

	record, err := dstSvc.Put(ctx, reader, stat.Size(), dstPath, nil)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		output.PrintJSON(vaultCpResponse{
			Path:          dstEp.raw,
			ObjectID:      record.ObjectKey,
			Size:          record.Size,
			ContentDigest: record.ContentDigest,
		})
	} else {
		fmt.Println(dstEp.raw)
		output.Printfln("Copied %d bytes → %s", record.Size, dstEp.raw)
	}
	return nil
}
