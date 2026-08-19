package vault

// CompiledVaultCreateToolName / CompiledVaultRestoreToolName are the
// compiler-backed names of the vault setup operations. They are surfaced by
// the operation catalog (not the CLI tree) and must route through the same
// out-of-band setup handlers as the legacy names so the create_url /
// restore_url + resume-handle hand-off contract is honored on the compiled
// surface.
const (
	CompiledVaultCreateToolName  = "vault_create"
	CompiledVaultRestoreToolName = "vault_restore"
)
