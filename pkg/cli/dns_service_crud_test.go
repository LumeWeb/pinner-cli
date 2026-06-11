package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func newUnauthDNSService(t *testing.T) *dnsServiceCLI {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
	return &dnsServiceCLI{
		ipfsServiceBase: ipfsServiceBase{cfgMgr: cfgMgr, authToken: ""},
	}
}

func newAuthedNilDNSService(t *testing.T) *dnsServiceCLI {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "token"}).Maybe()
	return &dnsServiceCLI{
		ipfsServiceBase: ipfsServiceBase{cfgMgr: cfgMgr, authToken: "token"},
		service:         nil,
	}
}

func TestDNSService_CreateZone_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.CreateZone(context.Background(), "example.com", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_CreateZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.CreateZone(context.Background(), "example.com", nil)
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_ListZones_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.ListZones(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_ListZones_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.ListZones(context.Background())
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_GetZone_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.GetZone(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_GetZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.GetZone(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_DeleteZone_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	err := svc.DeleteZone(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_DeleteZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	err := svc.DeleteZone(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_ValidateZone_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.ValidateZone(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_ValidateZone_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.ValidateZone(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_CreateRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.CreateRecord(context.Background(), "123", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_CreateRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.CreateRecord(context.Background(), "123", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_ListRecords_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.ListRecords(context.Background(), "123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_ListRecords_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.ListRecords(context.Background(), "123")
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_GetRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.GetRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_GetRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.GetRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_UpdateRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	_, err := svc.UpdateRecord(context.Background(), "123", "www", "A", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_UpdateRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	_, err := svc.UpdateRecord(context.Background(), "123", "www", "A", ipfs.RecordRequest{})
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestDNSService_DeleteRecord_Unauthenticated(t *testing.T) {
	svc := newUnauthDNSService(t)
	err := svc.DeleteRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDNSService_DeleteRecord_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilDNSService(t)
	err := svc.DeleteRecord(context.Background(), "123", "www", "A")
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}
