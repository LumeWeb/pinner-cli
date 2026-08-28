package cli

import (
	"context"
	"io"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// NopVaultService is a vault.VaultService whose every method is a no-op, so a
// concrete test stub only needs to embed it and override the methods it cares
// about. Adding a new method to vault.VaultService requires ONE no-op here
// instead of one hand-written no-op per test stub (previously three).
type NopVaultService struct{}

func (NopVaultService) CheckReady(context.Context) error { return nil }
func (NopVaultService) Put(context.Context, io.Reader, int64, string, map[string]any) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) Get(context.Context, string, io.Writer) error { return nil }
func (NopVaultService) List(context.Context, string) ([]vault.ListItem, error) {
	return nil, nil
}
func (NopVaultService) Search(context.Context, vault.SearchFilter) ([]vault.SearchItem, error) {
	return nil, nil
}
func (NopVaultService) Stat(context.Context, string) (*vault.StatResult, error) {
	return nil, nil
}
func (NopVaultService) Cat(context.Context, string, io.Writer) error { return nil }
func (NopVaultService) Verify(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (NopVaultService) VerifyDeep(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (NopVaultService) Remove(context.Context, string) error { return nil }
func (NopVaultService) VersionList(context.Context, string) ([]*vault.File, error) {
	return nil, nil
}
func (NopVaultService) VersionGet(context.Context, string, string) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) VersionDownload(context.Context, string, string, io.Writer) error {
	return nil
}
func (NopVaultService) VersionRestore(context.Context, string, string) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) AddTags(context.Context, string, []string) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) RemoveTags(context.Context, string, []string) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) SetTags(context.Context, string, []string) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) TagList(context.Context) ([]string, error) { return nil, nil }
func (NopVaultService) Share(context.Context, string, time.Time) (string, error) {
	return "", nil
}
func (NopVaultService) ShareAccept(context.Context, string, string, string) (*vault.File, error) {
	return nil, nil
}
func (NopVaultService) Sync(context.Context) (int, bool, error) { return 0, false, nil }
func (NopVaultService) Status(context.Context) (*vault.StatusResult, error) {
	return nil, nil
}
func (NopVaultService) Close() error { return nil }
