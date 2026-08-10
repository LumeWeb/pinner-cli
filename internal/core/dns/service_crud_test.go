package dns

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	"go.uber.org/zap"
)

func newUnauthService(t *testing.T) *serviceCLI {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
	return &serviceCLI{
		Base: ipfsbase.New(cfgMgr),
		log:  zap.NewNop(),
	}
}

func newAuthedNilService(t *testing.T) *serviceCLI {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "token"}).Maybe()
	return &serviceCLI{
		Base:    ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("token")),
		service: nil,
		log:     zap.NewNop(),
	}
}

func TestService_CreateZone_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.CreateZone(context.Background(), "example.com", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_CreateZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.CreateZone(context.Background(), "example.com", nil)
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_ListZones_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.ListZones(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_ListZones_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.ListZones(context.Background())
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_GetZone_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.GetZone(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_GetZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.GetZone(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_DeleteZone_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	err := svc.DeleteZone(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_DeleteZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	err := svc.DeleteZone(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_ValidateZone_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.ValidateZone(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_ValidateZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.ValidateZone(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_CreateRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.CreateRecord(context.Background(), "123", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_CreateRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.CreateRecord(context.Background(), "123", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_ListRecords_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.ListRecords(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_ListRecords_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.ListRecords(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_GetRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.GetRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_GetRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.GetRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_UpdateRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	_, err := svc.UpdateRecord(context.Background(), "123", "www", "A", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_UpdateRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	_, err := svc.UpdateRecord(context.Background(), "123", "www", "A", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestService_DeleteRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthService(t)
	err := svc.DeleteRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestService_DeleteRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilService(t)
	err := svc.DeleteRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}
