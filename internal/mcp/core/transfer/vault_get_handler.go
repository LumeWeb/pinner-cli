package transfer

// This file is the CLI-owned remainder of the download presentation: only the
// vault-read executor type survives locally. Every sink-routing /
// naming / size-cap / root-resolution helper (DownloadSinksAllowed,
// SinkDefaultName, ResolveLocalOutputPath, WriteLocalDownload, ExecuteLocalSink,
// ExecuteDropSink, ResolveDownloadRoot, DefaultSourceName) and the local-path
// executor type (IPFSDownloadHandler) are aliased onto go.lumeweb.com/pinner/
// transfer in bridge.go — the module owns their behavior.

// VaultGetHandler streams a single encrypted vault file (vault:/...) to dest.
// It is the authenticated vault-read executor, homed in the CLI layer where
// the vault service lives (mirror of VaultPutHandler). profile is the vault
// profile the object is read from; an empty value means the active/default
// profile.
import (
	"context"
	"io"
)

type VaultGetHandler func(ctx context.Context, vaultPath, profile string, w io.Writer) error
