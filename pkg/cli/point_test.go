package cli

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestPoint(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		cid         string
		setupIPNS   func(*mockIPNSServiceForCLI)
		wantErr     bool
		errContains string
		check       func(t *testing.T, buf *bytes.Buffer)
	}{
		{
			name:   "successful new point .eth",
			domain: "vitalik.eth",
			cid:    "bafybeigtest",
			setupIPNS: func(svc *mockIPNSServiceForCLI) {
				svc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
					return &ipfs.IPNSKeyResponse{Id: 1, Name: name, IpnsName: "k51qzi5uqu5djx", PeerId: "12D3KooW...", Created: time.Now()}, nil
				}
				svc.publishFunc = func(ctx context.Context, cid, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx", Value: cid, Published: time.Now(), Sequence: 1, Validity: time.Now().Add(24 * time.Hour)}, nil
				}
			},
			check: func(t *testing.T, buf *bytes.Buffer) {
				s := buf.String()
				require.Contains(t, s, "IPNS key published")
				require.Contains(t, s, "vitalik.eth")
				require.Contains(t, s, "bafybeigtest")
				require.Contains(t, s, "k51qzi5uqu5djx")
				require.Contains(t, s, "ipns://k51qzi5uqu5djx")
				require.Contains(t, s, "Set contenthash")
				require.Contains(t, s, "https://vitalik.eth.limo")
			},
		},
		{
			name:   "non-eth domain shows ipfs gateway verify",
			domain: "brave.crypto",
			cid:    "bafybeigtest",
			setupIPNS: func(svc *mockIPNSServiceForCLI) {
				svc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
					return &ipfs.IPNSKeyResponse{Id: 1, Name: name, IpnsName: "k51qzi5uqu5djx", PeerId: "12D3KooW...", Created: time.Now()}, nil
				}
				svc.publishFunc = func(ctx context.Context, cid, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx", Value: cid, Published: time.Now(), Sequence: 1, Validity: time.Now().Add(24 * time.Hour)}, nil
				}
			},
			check: func(t *testing.T, buf *bytes.Buffer) {
				s := buf.String()
				require.Contains(t, s, "https://k51qzi5uqu5djx.ipns.inbrowser.link")
			},
		},
		{
			name:   "key reuse",
			domain: "vitalik.eth",
			cid:    "bafybeigtest",
			setupIPNS: func(svc *mockIPNSServiceForCLI) {
				svc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
					return nil, fmt.Errorf("key already exists")
				}
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{{Id: 1, Name: "vitalik.eth", IpnsName: "k51qzi5uexisting", PeerId: "12D3KooW...", Created: time.Now()}}, nil
				}
				svc.publishFunc = func(ctx context.Context, cid, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
					return &ipfs.IPNSPublishResponse{Name: "k51qzi5uexisting", Value: cid, Published: time.Now(), Sequence: 2, Validity: time.Now().Add(24 * time.Hour)}, nil
				}
			},
			check: func(t *testing.T, buf *bytes.Buffer) {
				require.Contains(t, buf.String(), "k51qzi5uexisting")
			},
		},
		{
			name:    "missing name",
			domain:  "",
			cid:     "bafybeigtest",
			wantErr: true,
			errContains: "name is required",
		},
		{
			name:   "not authenticated",
			domain: "vitalik.eth",
			cid:    "bafybeigtest",
			setupIPNS: func(svc *mockIPNSServiceForCLI) {},
			wantErr: true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ipnsSvc IPNSService
			if tt.name == "not authenticated" {
				ipnsSvc = &unauthenticatedIPNSService{}
			} else if tt.setupIPNS != nil {
				mock := &mockIPNSServiceForCLI{}
				tt.setupIPNS(mock)
				ipnsSvc = mock
			}

			var buf bytes.Buffer
			output := NewOutputFormatter(false, false, false, false)
			output.SetWriter(&buf)

			err := pointWithServices(context.Background(), tt.domain, tt.cid, ipnsSvc, output)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, &buf)
			}
		})
	}
}

func TestPointJSON(t *testing.T) {
	ipnsSvc := &mockIPNSServiceForCLI{
		createKeyFunc: func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
			return &ipfs.IPNSKeyResponse{Id: 1, Name: name, IpnsName: "k51qzi5uqu5djx", PeerId: "12D3KooW...", Created: time.Now()}, nil
		},
		publishFunc: func(ctx context.Context, cid, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
			return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx", Value: cid, Published: time.Now(), Sequence: 1, Validity: time.Now().Add(24 * time.Hour)}, nil
		},
	}

	var buf bytes.Buffer
	output := NewOutputFormatter(true, false, false, false)
	output.SetWriter(&buf)

	err := pointWithServices(context.Background(), "vitalik.eth", "bafybeigtest", ipnsSvc, output)
	require.NoError(t, err)

	result := buf.String()
	require.Contains(t, result, `"vitalik.eth"`)
	require.Contains(t, result, `"ipns://k51qzi5uqu5djx"`)
	require.Contains(t, result, `"created": true`)
	require.Contains(t, result, `"verify_url": "https://vitalik.eth.limo"`)
}

func TestUnpoint(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		setupIPNS   func(*mockIPNSServiceForCLI)
		wantErr     bool
		errContains string
		check       func(t *testing.T, buf *bytes.Buffer)
	}{
		{
			name:   "successful unpoint",
			domain: "vitalik.eth",
			setupIPNS: func(svc *mockIPNSServiceForCLI) {
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{{Id: 1, Name: "vitalik.eth", IpnsName: "k51qzi5uqu5djx", PeerId: "12D3KooW...", Created: time.Now()}}, nil
				}
				svc.deleteKeyFunc = func(ctx context.Context, id string) error { return nil }
			},
			check: func(t *testing.T, buf *bytes.Buffer) {
				s := buf.String()
				require.Contains(t, s, "IPNS key removed")
				require.Contains(t, s, "vitalik.eth")
				require.Contains(t, s, "k51qzi5uqu5djx")
			},
		},
		{
			name:   "key not found",
			domain: "nexist.eth",
			setupIPNS: func(svc *mockIPNSServiceForCLI) {
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{}, nil
				}
			},
			wantErr:     true,
			errContains: "no IPNS key found",
		},
		{
			name:    "missing name",
			domain:  "",
			wantErr: true,
			errContains: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockIPNSServiceForCLI{}
			if tt.setupIPNS != nil {
				tt.setupIPNS(mock)
			}

			var buf bytes.Buffer
			output := NewOutputFormatter(false, false, false, false)
			output.SetWriter(&buf)

			err := unpointWithServices(context.Background(), tt.domain, mock, output)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, &buf)
			}
		})
	}
}

func TestUnpointJSON(t *testing.T) {
	ipnsSvc := &mockIPNSServiceForCLI{
		listKeysFunc: func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return []ipfs.IPNSKeyResponse{{Id: 1, Name: "vitalik.eth", IpnsName: "k51qzi5uqu5djx", PeerId: "12D3KooW...", Created: time.Now()}}, nil
		},
		deleteKeyFunc: func(ctx context.Context, id string) error { return nil },
	}

	var buf bytes.Buffer
	output := NewOutputFormatter(true, false, false, false)
	output.SetWriter(&buf)

	err := unpointWithServices(context.Background(), "vitalik.eth", ipnsSvc, output)
	require.NoError(t, err)

	result := buf.String()
	require.Contains(t, result, `"vitalik.eth"`)
	require.Contains(t, result, `"k51qzi5uqu5djx"`)
	require.Contains(t, result, `"deleted": true`)
}

func TestResolveVerifyURL(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		ipnsName string
		want     string
	}{
		{"eth name", "vitalik.eth", "k51qzi5uqu5djx", "https://vitalik.eth.limo"},
		{"non-eth name", "brave.crypto", "k51qzi5uqu5djx", "https://k51qzi5uqu5djx.ipns.inbrowser.link"},
		{"nested eth", "sub.vitalik.eth", "k51qzi5uqu5djx", "https://sub.vitalik.eth.limo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVerifyURL(tt.domain, tt.ipnsName)
			require.Equal(t, tt.want, got)
		})
	}
}
