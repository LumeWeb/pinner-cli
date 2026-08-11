package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"
)

func newVaultCatCommand() *cli.Command {
	return &cli.Command{
		Name:      "cat",
		Usage:     "Stream file content to stdout",
		ArgsUsage: vaultArgsUsageFile,
		Description: `Stream a vault file's raw content directly to stdout.

Progress and metadata go to stderr, so the stdout stream is the file bytes alone.
Does NOT return file metadata or directory listings: use vault stat for metadata, vault ls for listings.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				return fmt.Errorf("vault path required")
			}

			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			cfgMgr, err := configManagerFactory()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetUploadTimeout())
			defer cancel()

			// Agent/MCP mode must return structured content. Writing raw bytes to
			// stdout would corrupt the JSON-RPC transport and binary data cannot be
			// represented safely as MCP text content.
			if c.Bool(FlagAgent) {
				const maxAgentCatBytes = 4 * 1024 * 1024
				var content bytes.Buffer
				bw := &limitedBufferWriter{Buffer: &content, Limit: maxAgentCatBytes + 1}
				if err := svc.Cat(ctx, vaultPath, bw); err != nil && !errors.Is(err, io.ErrShortWrite) {
					return err
				}
				if bw.overflow || content.Len() > maxAgentCatBytes {
					return fmt.Errorf("vault file exceeds agent read limit of %d bytes", maxAgentCatBytes)
				}
				output.PrintJSON(map[string]any{
					"path":     vaultPath,
					"encoding": "base64",
					"size":     content.Len(),
					"content":  base64.StdEncoding.EncodeToString(content.Bytes()),
				})
				return nil
			}

			// Local CLI mode streams raw bytes to the command's configured writer.
			if err := svc.Cat(ctx, vaultPath, c.Root().Writer); err != nil {
				return err
			}
			return nil
		},
	}
}

type limitedBufferWriter struct {
	*bytes.Buffer
	Limit    int
	overflow bool
}

func (w *limitedBufferWriter) Write(p []byte) (int, error) {
	if w.Buffer.Len() >= w.Limit {
		w.overflow = true
		return 0, io.ErrShortWrite
	}
	remaining := w.Limit - w.Buffer.Len()
	if len(p) > remaining {
		w.overflow = true
		_, _ = w.Buffer.Write(p[:remaining])
		return remaining, io.ErrShortWrite
	}
	return w.Buffer.Write(p)
}
