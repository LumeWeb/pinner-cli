package transfer

import "fmt"

// ErrSourceModeUnavailable is the ONE shared source-mode/transport mismatch
// constructor for the upload tools' transport switches (upload_file and
// vault_put_file, stdio/http branches plus their OpenAI-tunnel variants): one
// error wording, six call sites, so the human-facing text can never drift
// between the two tools or between transports. The stdio/http branches pass
// the running transport kind; the OpenAI-tunnel branches pass the
// hand-written "OpenAI tunnel" label so their longer wording
// ("the OpenAI tunnel transport") stays stable.
func ErrSourceModeUnavailable(mode FileSourceMode, transport TransportKind) error {
	return fmt.Errorf("source mode %q is not available on the %s transport", mode, transport)
}
