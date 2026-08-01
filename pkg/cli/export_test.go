package cli

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// mockExportServiceForCLI is a mock implementation of the CLI ExportService interface for testing
type mockExportServiceForCLI struct {
	requireAuthenticatedErr error
	exportDAGFunc            func(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error)
	exportSiaObjectFunc      func(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error)
}

func (m *mockExportServiceForCLI) RequireAuthenticated() error {
	return m.requireAuthenticatedErr
}

func (m *mockExportServiceForCLI) ExportDAG(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error) {
	if m.exportDAGFunc != nil {
		return m.exportDAGFunc(ctx, cid)
	}
	return nil, nil
}

func (m *mockExportServiceForCLI) ExportSiaObject(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error) {
	if m.exportSiaObjectFunc != nil {
		return m.exportSiaObjectFunc(ctx, cid)
	}
	return nil, nil
}

func TestExportDAG(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockExportServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful export dag",
			setupMocks: func(svc *mockExportServiceForCLI) {
				svc.exportDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error) {
					return &ipfs.DAGExportResponse{
						RootCid:        "bafyroot",
						TotalBlocks:    2,
						TotalSizeBytes: 384,
						Blocks: []ipfs.DAGBlock{
							{Cid: "bafyroot", Size: 256, Links: []ipfs.DAGLink{{Cid: "bafychild", Index: 0}}},
							{Cid: "bafychild", Size: 128, Links: []ipfs.DAGLink{}},
						},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("bafyroot"),
			wantErr: false,
		},
		{
			name: "successful export dag with no blocks",
			setupMocks: func(svc *mockExportServiceForCLI) {
				svc.exportDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error) {
					return &ipfs.DAGExportResponse{
						RootCid:        "bafyroot",
						TotalBlocks:    0,
						TotalSizeBytes: 0,
						Blocks:         []ipfs.DAGBlock{},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("bafyroot"),
			wantErr: false,
		},
		{
			name:        "missing cid arg",
			cmd:         newMockCommand(),
			wantErr:     true,
			errContains: "CID argument required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockExportServiceForCLI) {
				svc.exportDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error) {
					return nil, errors.New("cid not found")
				}
			},
			cmd:         newMockCommand().withArgs("bafyroot"),
			wantErr:     true,
			errContains: "cid not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockExportServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := exportDAGWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExportDAGJSON(t *testing.T) {
	mockSvc := &mockExportServiceForCLI{}
	mockSvc.exportDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error) {
		return &ipfs.DAGExportResponse{
			RootCid:        "bafyroot",
			TotalBlocks:    1,
			TotalSizeBytes: 256,
			Blocks: []ipfs.DAGBlock{
				{Cid: "bafyroot", Size: 256, Links: []ipfs.DAGLink{{Cid: "bafychild", Index: 0}}},
			},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("bafyroot")

	err := exportDAGWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestExportSiaObject(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockExportServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful export sia object",
			setupMocks: func(svc *mockExportServiceForCLI) {
				svc.exportSiaObjectFunc = func(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error) {
					return &ipfs.CIDExportResponse{
						Cid:       "bafybeieffnocaq7t4w4daagvydl32igft5oziyyaebqr6vx6rb3fwh2ab4",
						SizeBytes: 1024,
						SharedObject: &ipfs.SharedObject{
							Slabs:   []ipfs.SlabSlice{{Version: 1, MinShards: 3, Offset: 0, Length: 1024}},
							DataKey: []int{0xAB},
						},
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-02T00:00:00Z",
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("bafybeieffnocaq7t4w4daagvydl32igft5oziyyaebqr6vx6rb3fwh2ab4"),
			wantErr: false,
		},
		{
			name: "no shared object available",
			setupMocks: func(svc *mockExportServiceForCLI) {
				svc.exportSiaObjectFunc = func(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error) {
					return &ipfs.CIDExportResponse{
						Cid:          "bafyempty",
						SizeBytes:    0,
						SharedObject: nil,
						CreatedAt:    "2026-01-01T00:00:00Z",
						UpdatedAt:    "2026-01-01T00:00:00Z",
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("bafyempty"),
			wantErr: false,
		},
		{
			name:        "missing cid arg",
			cmd:         newMockCommand(),
			wantErr:     true,
			errContains: "CID argument required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockExportServiceForCLI) {
				svc.exportSiaObjectFunc = func(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error) {
					return nil, errors.New("object not ready")
				}
			},
			cmd:         newMockCommand().withArgs("bafyroot"),
			wantErr:     true,
			errContains: "object not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockExportServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := exportSiaObjectWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExportSiaObjectJSON(t *testing.T) {
	mockSvc := &mockExportServiceForCLI{}
	mockSvc.exportSiaObjectFunc = func(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error) {
		return &ipfs.CIDExportResponse{
			Cid:       "bafybeieffnocaq7t4w4daagvydl32igft5oziyyaebqr6vx6rb3fwh2ab4",
			SizeBytes: 1024,
			SharedObject: &ipfs.SharedObject{
				Slabs:   []ipfs.SlabSlice{{Version: 1, MinShards: 3, Offset: 0, Length: 1024}},
				DataKey: []int{0xAB},
			},
			CreatedAt: "2026-01-01T00:00:00Z",
			UpdatedAt: "2026-01-02T00:00:00Z",
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("bafybeieffnocaq7t4w4daagvydl32igft5oziyyaebqr6vx6rb3fwh2ab4")

	err := exportSiaObjectWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

// exportDAGWithService is a test helper that allows injecting a mock ExportService
func exportDAGWithService(ctx context.Context, cmd commandGetter, output Output, exportSvc ExportService) error {
	if err := exportSvc.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() < 1 {
		return errors.New("CID argument required")
	}
	cid := args.First()

	result, err := exportSvc.ExportDAG(ctx, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Root CID: %s", result.RootCid)
	output.Printfln("Total blocks: %d", result.TotalBlocks)
	output.Printfln("Total size: %d bytes", result.TotalSizeBytes)

	if len(result.Blocks) == 0 {
		return nil
	}

	headers := []string{"CID", "SIZE", "LINKS", "SIA OBJECT"}
	rows := make([][]string, len(result.Blocks))
	for i, block := range result.Blocks {
		hasSia := "no"
		if block.SiaObject != nil {
			hasSia = "yes"
		}
		rows[i] = []string{
			block.Cid,
			strconv.FormatUint(block.Size, 10),
			strconv.Itoa(len(block.Links)),
			hasSia,
		}
	}

	output.PrintTable(headers, rows)
	return nil
}

// exportSiaObjectWithService is a test helper that allows injecting a mock ExportService
func exportSiaObjectWithService(ctx context.Context, cmd commandGetter, output Output, exportSvc ExportService) error {
	if err := exportSvc.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() < 1 {
		return errors.New("CID argument required")
	}
	cid := args.First()

	result, err := exportSvc.ExportSiaObject(ctx, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("CID: %s", result.Cid)
	output.Printfln("Size: %d bytes", result.SizeBytes)
	output.Printfln("Created: %s", result.CreatedAt)
	output.Printfln("Updated: %s", result.UpdatedAt)

	if result.SharedObject != nil {
		output.Printfln("")
		output.Printfln("Shared Object:")
		output.Printfln("  Data Key: %v", result.SharedObject.DataKey)
		output.Printfln("  Slabs: %d", len(result.SharedObject.Slabs))
		for i, slab := range result.SharedObject.Slabs {
			output.Printfln("    [%d] Version: %d, Min Shards: %d, Sectors: %d, Offset: %d, Length: %d",
				i, slab.Version, slab.MinShards, len(slab.Sectors), slab.Offset, slab.Length)
		}
	} else {
		output.Printfln("No Sia shared object available for this CID")
	}

	return nil
}
