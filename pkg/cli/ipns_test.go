package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestIPNSKeysList(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list keys",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{
						{
							Id:       1,
							Name:     "my-key",
							IpnsName: "k51qzi5uqu5djx123",
							PeerId:   "12D3KooWABC123",
							Created:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
						},
						{
							Id:       2,
							Name:     "another-key",
							IpnsName: "k51qzi5uqu5djx456",
							PeerId:   "12D3KooWDEF456",
							Created:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
						},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "no keys found",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "service error",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return nil, errors.New("failed to list keys")
				}
			},
			wantErr:     true,
			errContains: "failed to list keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := &cli.Command{}

			err := ipnsKeysListWithService(context.Background(), cmd, output, mockSvc)

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

func ipnsKeysListWithService(ctx context.Context, cmd *cli.Command, output Output, ipnsService IPNSService) error {
	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	keys, err := ipnsService.ListKeys(ctx)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		output.Printf("No IPNS keys found")
		return nil
	}

	output.Printf("Found %d IPNS key(s)", len(keys))

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
	rows := make([][]string, len(keys))
	for i, key := range keys {
		rows[i] = []string{
			fmt.Sprintf("%d", key.Id),
			key.Name,
			key.IpnsName,
			key.PeerId,
			key.Created.Format("2006-01-02 15:04:05"),
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

func TestIPNSResolve(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSResolveCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful resolve",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
					return &ipfs.IPNSResolveResponse{
						Name:     "k51qzi5uqu5djx123",
						Value:    "QmXxx",
						Sequence: 1,
						Expired:  false,
						Expires:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd:     &mockIPNSResolveCommand{ipnsName: "k51qzi5uqu5djx123"},
			wantErr: false,
		},
		{
			name: "successful resolve with expired record",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
					return &ipfs.IPNSResolveResponse{
						Name:     "k51qzi5uqu5djx456",
						Value:    "QmYyy",
						Sequence: 2,
						Expired:  true,
						Expires:  time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
					}, nil
				}
			},
			cmd:     &mockIPNSResolveCommand{ipnsName: "k51qzi5uqu5djx456"},
			wantErr: false,
		},
		{
			name:        "missing IPNS name",
			cmd:         &mockIPNSResolveCommand{ipnsName: ""},
			wantErr:     true,
			errContains: "IPNS name is required",
		},
		{
			name: "service error - invalid IPNS name",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
					return nil, errors.New("invalid IPNS name format")
				}
			},
			cmd:         &mockIPNSResolveCommand{ipnsName: "invalid"},
			wantErr:     true,
			errContains: "invalid IPNS name format",
		},
		{
			name: "service error - IPNS name not found",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
					return nil, errors.New("IPNS name not found")
				}
			},
			cmd:         &mockIPNSResolveCommand{ipnsName: "k51qzi5uqu5djx999"},
			wantErr:     true,
			errContains: "IPNS name not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsResolveWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestIPNSResolveJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSResolveCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful resolve JSON output",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
					return &ipfs.IPNSResolveResponse{
						Name:     "k51qzi5uqu5djx123",
						Value:    "QmXxx",
						Sequence: 1,
						Expired:  false,
						Expires:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd:     &mockIPNSResolveCommand{ipnsName: "k51qzi5uqu5djx123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsResolveWithService(context.Background(), tt.cmd, output, mockSvc)

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

type mockIPNSResolveCommand struct {
	ipnsName string
}

func (m *mockIPNSResolveCommand) Args() cli.Args {
	if m.ipnsName == "" {
		return &mockArgs{}
	}
	return &mockArgs{[]string{m.ipnsName}}
}

func ipnsResolveWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, ipnsService IPNSService) error {
	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("IPNS name is required")
	}

	ipnsName := args.First()
	if ipnsName == "" {
		return fmt.Errorf("IPNS name is required")
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	response, err := ipnsService.Resolve(ctx, ipnsName)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printf("IPNS name %s resolves to CID %s", response.Name, response.Value)

	headers := []string{"NAME", "CID", "SEQUENCE", "EXPIRED", "EXPIRES"}
	rows := [][]string{
		{
			response.Name,
			response.Value,
			fmt.Sprintf("%d", response.Sequence),
			fmt.Sprintf("%t", response.Expired),
			response.Expires.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func TestIPNSKeysCreate(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSCreateCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful create key",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
					return &ipfs.IPNSKeyResponse{
						Id:       1,
						Name:     name,
						IpnsName: "k51qzi5uqu5djx123",
						PeerId:   "12D3KooWABC123",
						Created:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "successful create key with import",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
					return &ipfs.IPNSKeyResponse{
						Id:       2,
						Name:     name,
						IpnsName: "k51qzi5uqu5djx456",
						PeerId:   "12D3KooWDEF456",
						Created:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "missing name",
			cmd:         &mockIPNSCreateCommand{name: ""},
			wantErr:     true,
			errContains: "name is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
					return nil, errors.New("failed to create key")
				}
			},
			cmd:         &mockIPNSCreateCommand{name: "my-key"},
			wantErr:     true,
			errContains: "failed to create key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := tt.cmd
			if cmd == nil {
				cmd = &mockIPNSCreateCommand{name: "my-key"}
			}

			err := ipnsKeysCreateWithService(context.Background(), cmd, output, mockSvc)

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

type mockIPNSCreateCommand struct {
	name string
	key  string
}

func (m *mockIPNSCreateCommand) String(name string) string {
	switch name {
	case FlagName:
		return m.name
	case "key":
		return m.key
	default:
		return ""
	}
}

func ipnsKeysCreateWithService(ctx context.Context, cmd interface{ String(name string) string }, output Output, ipnsService IPNSService) error {
	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	name := cmd.String(FlagName)
	if name == "" {
		return fmt.Errorf("name is required")
	}

	var key *string
	keyValue := cmd.String("key")
	if keyValue != "" {
		key = &keyValue
	}

	createdKey, err := ipnsService.CreateKey(ctx, name, key)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(createdKey)
	}

	output.Printf("Successfully created IPNS key")

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", createdKey.Id),
			createdKey.Name,
			createdKey.IpnsName,
			createdKey.PeerId,
			createdKey.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func TestIPNSKeysGet(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSGetCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful get key by numeric ID",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.getKeyFunc = func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
					return &ipfs.IPNSKeyResponse{
						Id:       1,
						Name:     "my-key",
						IpnsName: "k51qzi5uqu5djx123",
						PeerId:   "12D3KooWABC123",
						Created:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd:     &mockIPNSGetCommand{keyArg: "1"},
			wantErr: false,
		},
		{
			name: "successful get key by name",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.getKeyFunc = func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
					return &ipfs.IPNSKeyResponse{
						Id:       2,
						Name:     "another-key",
						IpnsName: "k51qzi5uqu5djx456",
						PeerId:   "12D3KooWDEF456",
						Created:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{
						{Id: 2, Name: "another-key", IpnsName: "k51qzi5uqu5djx456", PeerId: "12D3KooWDEF456", Created: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)},
					}, nil
				}
			},
			cmd:     &mockIPNSGetCommand{keyArg: "another-key"},
			wantErr: false,
		},
		{
			name:        "missing key arg",
			cmd:         &mockIPNSGetCommand{keyArg: ""},
			wantErr:     true,
			errContains: "key name or ID is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.getKeyFunc = func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
					return nil, errors.New("failed to get key")
				}
			},
			cmd:         &mockIPNSGetCommand{keyArg: "1"},
			wantErr:     true,
			errContains: "failed to get key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsKeysGetWithService(context.Background(), tt.cmd, output, mockSvc)

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

type mockIPNSGetCommand struct {
	keyArg string
}

func (m *mockIPNSGetCommand) String(name string) string {
	return ""
}

func (m *mockIPNSGetCommand) Args() cli.Args {
	if m.keyArg == "" {
		return &mockArgs{}
	}
	return &mockArgs{[]string{m.keyArg}}
}

func ipnsKeysGetWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, ipnsService IPNSService) error {
	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key name or ID is required")
	}

	keyArg := args.First()
	if keyArg == "" {
		return fmt.Errorf("key name or ID is required")
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	keyID, err := resolveIPNSKeyIDToString(ctx, ipnsService, keyArg)
	if err != nil {
		return err
	}

	key, err := ipnsService.GetKey(ctx, keyID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(key)
	}

	output.Printf("IPNS Key Details")

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", key.Id),
			key.Name,
			key.IpnsName,
			key.PeerId,
			key.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func TestIPNSKeysDelete(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSGetCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful delete key by ID",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.deleteKeyFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			cmd:     &mockIPNSGetCommand{keyArg: "1"},
			wantErr: false,
		},
		{
			name: "successful delete key by name",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.deleteKeyFunc = func(ctx context.Context, id string) error {
					return nil
				}
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{
						{Id: 2, Name: "my-key", IpnsName: "k51qzi5uqu5djx456", PeerId: "12D3KooWDEF456", Created: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)},
					}, nil
				}
			},
			cmd:     &mockIPNSGetCommand{keyArg: "my-key"},
			wantErr: false,
		},
		{
			name:        "missing key arg",
			cmd:         &mockIPNSGetCommand{keyArg: ""},
			wantErr:     true,
			errContains: "key name or ID is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.deleteKeyFunc = func(ctx context.Context, id string) error {
					return errors.New("failed to delete key")
				}
			},
			cmd:         &mockIPNSGetCommand{keyArg: "1"},
			wantErr:     true,
			errContains: "failed to delete key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsKeysDeleteWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestIPNSKeysDeleteJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSGetCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful delete key JSON output",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.deleteKeyFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			cmd:     &mockIPNSGetCommand{keyArg: "1"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsKeysDeleteWithService(context.Background(), tt.cmd, output, mockSvc)

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

func ipnsKeysDeleteWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, ipnsService IPNSService) error {
	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key name or ID is required")
	}

	keyArg := args.First()

	keyID, err := resolveIPNSKeyIDToString(ctx, ipnsService, keyArg)
	if err != nil {
		return err
	}

	if err := ipnsService.DeleteKey(ctx, keyID); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"success": true,
			"message": fmt.Sprintf("IPNS key %s deleted successfully", keyArg),
		}
		return output.PrintJSON(result)
	}

	output.Printf("IPNS key %s deleted successfully", keyArg)

	return nil
}

func TestIPNSPublish(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSPublishCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful publish with key name",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return &ipfs.IPNSPublishResponse{
						Name:      "k51qzi5uqu5djx123",
						Value:     "QmXxx",
						Published: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
						Sequence:  1,
						Validity:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd: &mockIPNSPublishCommand{
				cid:     "QmXxx",
				keyName: "my-key",
				ttl:     "",
			},
			wantErr: false,
		},
		{
			name: "successful publish with TTL",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return &ipfs.IPNSPublishResponse{
						Name:      "k51qzi5uqu5djx456",
						Value:     "QmYyy",
						Published: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
						Sequence:  2,
						Validity:  time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd: &mockIPNSPublishCommand{
				cid:     "QmYyy",
				keyName: "another-key",
				ttl:     "24h",
			},
			wantErr: false,
		},
		{
			name: "missing CID",
			cmd: &mockIPNSPublishCommand{
				cid:     "",
				keyName: "my-key",
			},
			wantErr:     true,
			errContains: "CID is required",
		},
		{
			name: "missing key name",
			cmd: &mockIPNSPublishCommand{
				cid:     "QmXxx",
				keyName: "",
			},
			wantErr:     true,
			errContains: "key-name is required",
		},
		{
			name: "service error - invalid CID",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return nil, errors.New("invalid CID format")
				}
			},
			cmd: &mockIPNSPublishCommand{
				cid:     "invalid",
				keyName: "my-key",
			},
			wantErr:     true,
			errContains: "invalid CID format",
		},
		{
			name: "service error - key not found",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return nil, errors.New("key not found")
				}
			},
			cmd: &mockIPNSPublishCommand{
				cid:     "QmXxx",
				keyName: "nonexistent",
			},
			wantErr:     true,
			errContains: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsPublishWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestIPNSPublishJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSPublishCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful publish JSON output",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return &ipfs.IPNSPublishResponse{
						Name:      "k51qzi5uqu5djx123",
						Value:     "QmXxx",
						Published: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
						Sequence:  1,
						Validity:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd: &mockIPNSPublishCommand{
				cid:     "QmXxx",
				keyName: "my-key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsPublishWithService(context.Background(), tt.cmd, output, mockSvc)

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

type mockIPNSPublishCommand struct {
	cid     string
	keyName string
	ttl     string
}

func (m *mockIPNSPublishCommand) Int(name string) int {
	return 0
}

func (m *mockIPNSPublishCommand) String(name string) string {
	switch name {
	case "key-name":
		return m.keyName
	case "ttl":
		return m.ttl
	default:
		return ""
	}
}

func (m *mockIPNSPublishCommand) Args() cli.Args {
	if m.cid == "" {
		return &mockArgs{}
	}
	return &mockArgs{[]string{m.cid}}
}

func ipnsPublishWithService(ctx context.Context, cmd interface {
	Int(name string) int
	String(name string) string
	Args() cli.Args
}, output Output, ipnsService IPNSService) error {
	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("CID is required")
	}

	cid := args.First()
	if cid == "" {
		return fmt.Errorf("CID is required")
	}

	keyName := cmd.String("key-name")
	if keyName == "" {
		return fmt.Errorf("key-name is required")
	}

	var ttl *string
	ttlValue := cmd.String("ttl")
	if ttlValue != "" {
		ttl = &ttlValue
	}

	response, err := ipnsService.Publish(ctx, cid, keyName, ttl)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printf("Published CID %s to IPNS name %s", response.Value, response.Name)

	headers := []string{"NAME", "VALUE", "PUBLISHED", "SEQUENCE", "VALIDITY"}
	rows := [][]string{
		{
			response.Name,
			response.Value,
			response.Published.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", response.Sequence),
			response.Validity.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func TestIPNSRepublish(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSRepublishCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful republish by name",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.republishFunc = func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
					return &ipfs.IPNSRepublishResponse{
						Count:   1,
						Message: "republished successfully",
					}, nil
				}
			},
			cmd:     &mockIPNSRepublishCommand{keyArg: "my-key"},
			wantErr: false,
		},
		{
			name: "successful republish by ID",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.republishFunc = func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
					return &ipfs.IPNSRepublishResponse{
						Count:   1,
						Message: "republished successfully",
					}, nil
				}
			},
			cmd:     &mockIPNSRepublishCommand{keyArg: "1"},
			wantErr: false,
		},
		{
			name:        "missing key arg",
			cmd:         &mockIPNSRepublishCommand{keyArg: ""},
			wantErr:     true,
			errContains: "key name or ID is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.republishFunc = func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
					return nil, errors.New("republish failed")
				}
			},
			cmd:         &mockIPNSRepublishCommand{keyArg: "my-key"},
			wantErr:     true,
			errContains: "republish failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsRepublishWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestIPNSRepublishJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockIPNSServiceForCLI)
		cmd         *mockIPNSRepublishCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful republish JSON output",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				svc.republishFunc = func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
					return &ipfs.IPNSRepublishResponse{
						Count:   1,
						Message: "republished successfully",
					}, nil
				}
			},
			cmd:     &mockIPNSRepublishCommand{keyArg: "my-key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := ipnsRepublishWithService(context.Background(), tt.cmd, output, mockSvc)

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

type mockIPNSRepublishCommand struct {
	keyArg string
}

func (m *mockIPNSRepublishCommand) Args() cli.Args {
	if m.keyArg == "" {
		return &mockArgs{}
	}
	return &mockArgs{[]string{m.keyArg}}
}

func ipnsRepublishWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, ipnsService IPNSService) error {
	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key name or ID is required")
	}

	keyArg := args.First()

	response, err := ipnsService.Republish(ctx, keyArg)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printf("Republished IPNS key %s: %s (%d record(s))", keyArg, response.Message, response.Count)

	return nil
}


