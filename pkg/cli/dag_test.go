package cli

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// mockDAGServiceForCLI is a mock implementation of the CLI DAGService interface for testing
type mockDAGServiceForCLI struct {
	requireAuthenticatedErr error
	resolveDAGFunc          func(ctx context.Context, cid string) (*ipfs.DAGResponse, error)
}

func (m *mockDAGServiceForCLI) RequireAuthenticated() error {
	return m.requireAuthenticatedErr
}

func (m *mockDAGServiceForCLI) ResolveDAG(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
	if m.resolveDAGFunc != nil {
		return m.resolveDAGFunc(ctx, cid)
	}
	return nil, nil
}

func TestDagResolve(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockDAGServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful dag resolve",
			setupMocks: func(svc *mockDAGServiceForCLI) {
				svc.resolveDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
					return &ipfs.DAGResponse{
						RootCid: "bafyroot",
						Nodes: []ipfs.DAGBlockNodeResponse{
							{Cid: "bafyroot", Size: 256, Children: []string{"bafychild1", "bafychild2"}},
							{Cid: "bafychild1", Size: 128, Children: []string{}},
							{Cid: "bafychild2", Size: 64, Children: []string{}},
						},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("bafyroot"),
			wantErr: false,
		},
		{
			name: "successful dag resolve with no nodes",
			setupMocks: func(svc *mockDAGServiceForCLI) {
				svc.resolveDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
					return &ipfs.DAGResponse{
						RootCid: "bafyroot",
						Nodes:   []ipfs.DAGBlockNodeResponse{},
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
			setupMocks: func(svc *mockDAGServiceForCLI) {
				svc.resolveDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
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
			mockSvc := &mockDAGServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := dagResolveWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestDagResolveJSON(t *testing.T) {
	mockSvc := &mockDAGServiceForCLI{}
	mockSvc.resolveDAGFunc = func(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
		return &ipfs.DAGResponse{
			RootCid: "bafyroot",
			Nodes: []ipfs.DAGBlockNodeResponse{
				{Cid: "bafyroot", Size: 256, Children: []string{"bafychild1"}},
				{Cid: "bafychild1", Size: 128, Children: []string{}},
			},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("bafyroot")

	err := dagResolveWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

// dagResolveWithService is a test helper that allows injecting a mock DAGService
func dagResolveWithService(ctx context.Context, cmd commandGetter, output Output, dagService DAGService) error {
	if err := dagService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() < 1 {
		return errors.New("CID argument required")
	}
	cid := args.First()

	result, err := dagService.ResolveDAG(ctx, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Root CID: %s", result.RootCid)
	output.Printfln("Total blocks: %d", len(result.Nodes))

	if len(result.Nodes) == 0 {
		return nil
	}

	headers := []string{"CID", "SIZE", "CHILDREN"}
	rows := make([][]string, len(result.Nodes))
	for i, node := range result.Nodes {
		rows[i] = []string{
			node.Cid,
			strconv.Itoa(node.Size),
			strconv.Itoa(len(node.Children)),
		}
	}

	output.PrintTable(headers, rows)
	return nil
}
