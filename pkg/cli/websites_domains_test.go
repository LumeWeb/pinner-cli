package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestWebsitesDomainsList(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list domains",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName},
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com"),
			wantErr: false,
		},
		{
			name: "list domains empty",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com"),
			wantErr: false,
		},
		{
			name: "list domains service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return nil, errors.New("failed to list domains")
				}
			},
			cmd:         newMockCommand().withArgs("example.com"),
			wantErr:     true,
			errContains: "failed to list domains",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsListWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesDomainsListJSON(t *testing.T) {
	zoneName := "example.com."
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "example.com"},
		}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName},
			{Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("example.com")

	err := websitesDomainsListWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsAdd(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful add domain",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "icann"),
			wantErr: false,
		},
		{
			name: "successful add domain with --website flag",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
					}, nil
				}
			},
			cmd:     newMockCommand().withString(FlagWebsite, "example.com").withArgs("mydomain.com").withString("namespace", "icann"),
			wantErr: false,
		},
		{
			name: "both --website flag and positional website errors",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return nil, nil
				}
			},
			cmd:         newMockCommand().withString(FlagWebsite, "example.com").withArgs("example.com", "mydomain.com"),
			wantErr:     true,
			errContains: "both as --website flag and positional",
		},
		{
			name: "successful add domain with hns namespace",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com", "mydomain.hns").withString("namespace", "hns"),
			wantErr: false,
		},
		{
			name: "invalid namespace",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "invalid"),
			wantErr:     true,
			errContains: `invalid namespace "invalid"`,
		},
		{
			name: "single domain auto-selects sole website",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{Id: 1, Domain: req.Domain, Namespace: req.Namespace}, nil
				}
			},
			cmd:     newMockCommand().withArgs("mydomain.com").withString("namespace", "icann"),
			wantErr: false,
		},
		{
			name: "single domain with multiple websites requires website",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
						{Id: 2, Domain: "other.com"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("mydomain.com").withString("namespace", "icann"),
			wantErr:     true,
			errContains: "multiple websites found",
		},
		{
			name: "no websites",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return nil, nil
				}
			},
			cmd:         newMockCommand().withArgs("mydomain.com"),
			wantErr:     true,
			errContains: "no websites found",
		},
		{
			name:        "missing args",
			cmd:         newMockCommand(),
			wantErr:     true,
			errContains: "usage: pinner websites domains add",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return nil, errors.New("bind failed")
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "icann"),
			wantErr:     true,
			errContains: "bind failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsAddWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesDomainsAddJSON(t *testing.T) {
	zoneName := "example.com."
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "example.com"},
		}, nil
	}
	mockSvc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "icann")

	err := websitesDomainsAddWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsRm(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful remove domain by numeric ID",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs(strconv.Itoa(42)),
			wantErr: false,
		},

		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 99, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return errors.New("domain not found")
				}
			},
			cmd:         newMockCommand().withArgs(strconv.Itoa(99)),
			wantErr:     true,
			errContains: "domain not found",
		},
		{
			name: "successful remove domain by domain name",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs("staging.example.com"),
			wantErr: false,
		},
		{
			name: "domain name not found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("missing.example.com"),
			wantErr:     true,
			errContains: `domain "missing.example.com" not found bound to any website`,
		},
		{
			name:        "missing arg",
			cmd:         newMockCommand(),
			wantErr:     true,
			errContains: "domain argument is required",
		},
		{
			name:        "extra positional args rejected",
			cmd:         newMockCommand().withArgs("example.com", "staging.example.com"),
			wantErr:     true,
			errContains: "unexpected extra argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsRmWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestResolveDomainBindingNumericNamePriority(t *testing.T) {
	// Regression for Kody finding: name matching must take priority over
	// numeric ID matching. A website that appears earlier in iteration with a
	// binding whose numeric ID is "123" must NOT shadow a later website that
	// has a domain literally named "123" (valid in namespaces like HNS).
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "earlier.com"},
			{Id: 2, Domain: "later.com"},
		}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		switch websiteID {
		case "1":
			// Earlier website: a binding with numeric ID "123" but a
			// non-numeric name. If ID matching short-circuits here, "rm 123"
			// would wrongly target this.
			return []ipfs.DomainResponse{{Id: 123, Domain: "alpha.hns", Namespace: "hns"}}, nil
		case "2":
			return []ipfs.DomainResponse{{Id: 7, Domain: "123", Namespace: "hns"}}, nil
		}
		return nil, nil
	}

	websiteID, domainID, err := resolveDomainBinding(context.Background(), mockSvc, "123")
	require.NoError(t, err)
	require.Equal(t, "2", websiteID)
	require.Equal(t, "7", domainID)
}

func TestResolveDomainBindingDeferredListErrors(t *testing.T) {
	// A ListDomains failure is deferred, not fatal: a website that fails to
	// list must not block an unambiguously name-matched domain on a later
	// website from resolving.
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "broken.com"},
			{Id: 2, Domain: "good.com"},
		}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		switch websiteID {
		case "1":
			return nil, errors.New("boom")
		case "2":
			return []ipfs.DomainResponse{{Id: 7, Domain: "staging.example.com", Namespace: "icann"}}, nil
		}
		return nil, nil
	}

	websiteID, domainID, err := resolveDomainBinding(context.Background(), mockSvc, "staging.example.com")
	require.NoError(t, err)
	require.Equal(t, "2", websiteID)
	require.Equal(t, "7", domainID)
}

func TestResolveDomainBindingSurfacesListErrors(t *testing.T) {
	// A ListDomains failure surfaces (wrapped) only when no name match is
	// found and no clean numeric-ID fallback exists — otherwise the error
	// would be silently masked as "not found".
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
	}
	wantErr := errors.New("boom")
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return nil, wantErr
	}

	_, _, err := resolveDomainBinding(context.Background(), mockSvc, "staging.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to look up domain on website")
	require.ErrorIs(t, err, wantErr)
}

func TestWebsitesDomainsRmJSON(t *testing.T) {
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
		}, nil
	}
	mockSvc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
		return nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("staging.example.com")

	err := websitesDomainsRmWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsVerify(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful verify domain",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 1, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("mydomain.com"),
			wantErr: false,
		},
		{
			name: "successful verify domain by name with trailing dot",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("mydomain.hns."),
			wantErr: false,
		},
		{
			name: "successful verify by numeric binding id",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("2"),
			wantErr: false,
		},
		{
			name: "domain name not found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("unknown.example.com"),
			wantErr:     true,
			errContains: `domain "unknown.example.com" not found bound to any website`,
		},
		{
			name:        "missing arg",
			cmd:         newMockCommand(),
			wantErr:     true,
			errContains: "domain argument is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return nil, errors.New("verify failed")
				}
			},
			cmd:         newMockCommand().withArgs("mydomain.com"),
			wantErr:     true,
			errContains: "verify failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsVerifyWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesDomainsVerifyJSON(t *testing.T) {
	zoneName := "example.com."
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 1, Domain: "mydomain.com", Namespace: "icann"},
		}, nil
	}
	mockSvc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("mydomain.com")

	err := websitesDomainsVerifyWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

// websitesDomainsListWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsListWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return errors.New("website ID or domain is required")
	}

	websiteID, err := resolveWebsiteID(ctx, websitesService, args.First())
	if err != nil {
		return err
	}

	domains, err := websitesService.ListDomains(ctx, websiteID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		if domains == nil {
			domains = []ipfs.DomainResponse{}
		}
		return output.PrintJSON(map[string]any{
			"count":   len(domains),
			"domains": domains,
		})
	}

	if len(domains) == 0 {
		output.Printfln("No domains found for website %s", websiteID)
		return nil
	}

	output.Printfln("Found %d domain(s) for website %s", len(domains), websiteID)

	headers := []string{"ID", "DOMAIN", "NAMESPACE", "STATUS", "ZONE NAME"}
	rows := make([][]string, len(domains))
	for i, d := range domains {
		zoneName := ""
		if d.ZoneName != nil {
			zoneName = *d.ZoneName
		}
		status := ""
		if d.Status != nil {
			status = *d.Status
		}
		rows[i] = []string{
			strconv.Itoa(d.Id),
			d.Domain,
			d.Namespace,
			status,
			zoneName,
		}
	}

	output.PrintTable(headers, rows)
	return nil
}

// websitesDomainsAddWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsAddWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID, domain, err := resolveAddTarget(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	namespace := cmd.String("namespace")
	if namespace != "icann" && namespace != "hns" {
		return fmt.Errorf("invalid namespace %q: must be 'icann' or 'hns'", namespace)
	}

	req := ipfs.DomainRequest{
		Domain:    domain,
		Namespace: namespace,
	}

	result, err := websitesService.BindDomain(ctx, websiteID, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Domain bound successfully")
	zoneName := ""
	if result.ZoneName != nil {
		zoneName = *result.ZoneName
	}
	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", strconv.Itoa(result.Id)},
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
			{"Zone Name", zoneName},
		},
	})

	return nil
}

// websitesDomainsRmWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsRmWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	domainArg, err := resolveDomainArg(cmd, "rm")
	if err != nil {
		return err
	}

	websiteID, domainID, err := resolveDomainBinding(ctx, websitesService, domainArg)
	if err != nil {
		return err
	}

	if err := websitesService.UnbindDomain(ctx, websiteID, domainID); err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"deleted":   true,
			"domain_id": domainID,
		})
	}

	output.Printfln("Domain %s removed from website %s", domainArg, websiteID)
	return nil
}

// websitesDomainsVerifyWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsVerifyWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	domainArg, err := resolveDomainArg(cmd, "verify")
	if err != nil {
		return err
	}

	websiteID, domainID, err := resolveDomainBinding(ctx, websitesService, domainArg)
	if err != nil {
		return err
	}

	result, err := websitesService.VerifyDomain(ctx, websiteID, domainID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Domain verification triggered")
	zoneName := ""
	if result.ZoneName != nil {
		zoneName = *result.ZoneName
	}
	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", strconv.Itoa(result.Id)},
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
			{"Zone Name", zoneName},
		},
	})

	return nil
}

func TestResolveDomainID(t *testing.T) {
	mkSvc := func(domains []ipfs.DomainResponse) *mockWebsitesServiceForCLI {
		svc := &mockWebsitesServiceForCLI{}
		svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return domains, nil
		}
		return svc
	}

	domains := []ipfs.DomainResponse{
		{Id: 1, Domain: "lumeweb", Namespace: "hns"},
		{Id: 2, Domain: "example.com", Namespace: "icann"},
	}

	tests := []struct {
		name      string
		arg       string
		want      string
		wantErr   bool
		errPrefix string
	}{
		{"exact name", "lumeweb", "1", false, ""},
		{"trailing dot arg matches bare stored name", "lumeweb.", "1", false, ""},
		{"case insensitive", "LUMEWEB", "1", false, ""},
		{"case insensitive trailing dot", "LUMEWEB.", "1", false, ""},
		{"icann domain", "example.com.", "2", false, ""},
		{"numeric id", "2", "2", false, ""},
		{"not found", "nonexistent", "", true, "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mkSvc(domains)
			got, err := resolveDomainID(context.Background(), svc, "1", tt.arg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errPrefix)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// websitesDomainsDNSRequirementsWithService is a test helper that allows
// injecting a mock WebsitesService. Mirrors websitesDomainsDNSRequirements.
func websitesDomainsDNSRequirementsWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	domainArg, err := resolveDomainArg(cmd, "dns-requirements")
	if err != nil {
		return err
	}

	websiteID, domainID, err := resolveDomainBinding(ctx, websitesService, domainArg)
	if err != nil {
		return err
	}

	result, err := websitesService.GetDomainDNSRequirements(ctx, websiteID, domainID)
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("no DNS requirements returned for domain %s", domainID)
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	managed := isWebsiteDNSManaged(ctx, websitesService, websiteID)
	renderDomainDelegation(output, result, managed)
	return nil
}

func TestWebsitesDomainsDNSRequirements(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful dns-requirements without delegation",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("mydomain.hns"),
			wantErr: false,
		},
		{
			name: "successful dns-requirements with delegation records",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
						Delegation: &ipfs.DNSDelegation{
							Mode:         strPtr("delegated"),
							Ds:           strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>"),
							Instructions: strPtr("Publish parent records in your HNS wallet."),
							ParentRecords: &[]ipfs.DNSDelegationRecord{
								{Type: "NS", Value: strPtr("ns1.lumeweb,ns2.lumeweb")},
								{Type: "DS", Value: strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>")},
							},
							AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
								{Type: "NS", Value: strPtr("ns1.lumeweb\nns2.lumeweb")},
								{Type: "TLSA", Value: strPtr("_443._tcp.mydomain. 3 1 1 <sha256>")},
							},
						},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("mydomain.hns"),
			wantErr: false,
		},
		{
			name: "successful by numeric binding id",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{Id: 2, Domain: "mydomain.hns", Namespace: "hns"}, nil
				}
			},
			cmd:     newMockCommand().withArgs("2"),
			wantErr: false,
		},
		{
			name: "domain name not found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("unknown.example.com"),
			wantErr:     true,
			errContains: `domain "unknown.example.com" not found bound to any website`,
		},
		{
			name:        "missing arg",
			cmd:         newMockCommand(),
			wantErr:     true,
			errContains: "domain argument is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return nil, errors.New("fetch failed")
				}
			},
			cmd:         newMockCommand().withArgs("mydomain.com"),
			wantErr:     true,
			errContains: "fetch failed",
		},
		{
			name: "nil result without error is guarded",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return nil, nil
				}
			},
			cmd:         newMockCommand().withArgs("mydomain.com"),
			wantErr:     true,
			errContains: "no DNS requirements returned for domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsDNSRequirementsWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesDomainsDNSRequirementsJSON(t *testing.T) {
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 1, Domain: "mydomain.hns", Namespace: "hns"},
		}, nil
	}
	mockSvc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				Ds:   strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>"),
			},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("mydomain.hns")

	err := websitesDomainsDNSRequirementsWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsDNSRequirementsUsesManagedSignal(t *testing.T) {
	// The handler must read the website's dns_hosting_enabled (via Get) and
	// omit the authoritative records when Pinner manages the DNS.
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
		}, nil
	}
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		return &ipfs.WebsiteItem{Id: 1, Domain: "example.com", DnsHostingEnabled: true}, nil
	}
	mockSvc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 2, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("nsx.pinner.xyz")},
				},
			},
		}, nil
	}

	var buf bytes.Buffer
	output := NewOutputFormatter(false, false, false, false)
	output.SetWriter(&buf)
	cmd := newMockCommand().withArgs("mydomain.hns")
	err := websitesDomainsDNSRequirementsWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Pinner manages your DNS")
	assert.NotContains(t, out, "Authoritative records")
	assert.NotContains(t, out, "nsx.pinner.xyz")
}

func TestRenderDomainDelegation(t *testing.T) {
	t.Run("renders no delegation message when nil", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
		}, false)
		// exercises the nil-delegation branch without asserting exact text
	})

	t.Run("renders records with typed helper", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode:         strPtr("delegated"),
				Ds:           strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>"),
				Instructions: strPtr("Publish parent records in your HNS wallet."),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb,ns2.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain. 3 1 1 <sha256>")},
				},
			},
		}, false)
		// exercises the non-nil typed-helper path
	})

	t.Run("inline mode labels authoritative records via synthetic nameservers", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("inline"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "SYNTH4", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
				},
			},
		}, false)
		// In inline mode the authoritative side is served automatically via
		// synthetic nameserver names — it is not user-configured.
	})

	t.Run("icann driver renders registrar wording and nameservers", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Nameservers:  &[]string{"ns1.example.com", "ns2.example.com"},
				Instructions: strPtr("Configure these NS records at your registrar for mydomain.com"),
			},
		}, false)
		// exercises the icann driver path (registrar wording, nameservers list)
	})

	t.Run("unknown namespace falls back to generic driver", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.eth", Namespace: "ens", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain.eth. 3 1 1 <sha256>")},
				},
			},
		}, false)
		// exercises the generic fallback path for an unrecognized namespace
	})

	t.Run("DS appears once in parent records and stays contiguous, comma-joined NS is split", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		// A full-length SHA-256 digest (64 hex chars) that exceeds the table's
		// default wrap width — it must render contiguous so it stays copyable.
		digest := "c35938688953467518f2a9c613b8a32da647595912a67fa9cf47e41b593831d5"
		dsValue := "lumeweb DS 44451 13 2 " + digest
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "lumeweb", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				Ds:   strPtr(dsValue),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz,ns2.pinner.xyz")},
					{Type: "DS", Value: strPtr(dsValue)},
				},
			},
		}, false)
		out := buf.String()
		// The DS record is communicated once, as a parent record in the
		// parent-records table — not re-decoded into a redundant block.
		assert.Equal(t, 1, strings.Count(out, dsValue))
		assert.NotContains(t, out, "DS record (paste")
		assert.NotContains(t, out, "KEY TAG")
		// The digest is never hard-wrapped mid-value: the full string appears
		// intact and contiguous so it can be selected and copied whole.
		assert.Contains(t, out, dsValue)
		assert.Equal(t, 1, strings.Count(out, digest))
		// Comma-joined nameservers are split so each is visible/copyable,
		// matching how the wizard communicates nameservers.
		assert.Contains(t, out, "ns1.pinner.xyz")
		assert.Contains(t, out, "ns2.pinner.xyz")
		assert.NotContains(t, out, "ns1.pinner.xyz,ns2.pinner.xyz")
	})

	t.Run("managed hns omits authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz")},
					{Type: "DS", Value: strPtr("44451 13 2 c359")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("nsx.pinner.xyz")},
				},
			},
		}, true)
		out := buf.String()
		// Pinner manages DNS, so only the parent records (for the HNS wallet)
		// are shown; the authoritative side is handled for the user.
		assert.Contains(t, out, "Pinner manages your DNS")
		assert.Contains(t, out, "Parent records (publish in your HNS wallet)")
		assert.Contains(t, out, "44451 13 2 c359")
		assert.NotContains(t, out, "Authoritative records")
		assert.NotContains(t, out, "nsx.pinner.xyz")
		// The server's free-form instructions prose is never echoed.
		assert.NotContains(t, out, "parent_records")
		assert.NotContains(t, out, "optional GLUE")
	})

	t.Run("managed icann omits authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain.com. 3 1 1 <sha256>")},
				},
			},
		}, true)
		out := buf.String()
		assert.Contains(t, out, "Point your registrar's nameservers")
		assert.Contains(t, out, "Pinner manages your DNS")
		assert.NotContains(t, out, "Authoritative records")
		assert.NotContains(t, out, "TLSA")
	})

	t.Run("self-managed hns shows authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns.eigen.lumeweb")},
				},
			},
		}, false)
		out := buf.String()
		assert.Contains(t, out, "point your own DNS server")
		assert.Contains(t, out, "Authoritative records (configure on your DNS server)")
		assert.Contains(t, out, "ns.eigen.lumeweb")
	})
}
