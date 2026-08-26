package websites

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// fakeService is a minimal Service test double implementing only the two
// methods the resolvers use (List and ListDomains). All other Service methods
// panic if called, which is fine for pure resolution tests.
type fakeService struct {
	Service
	listFn    func(ctx context.Context) ([]ipfs.WebsiteItem, error)
	domainsFn func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error)
}

func (f *fakeService) List(ctx context.Context, opts ListOptions) ([]ipfs.WebsiteItem, error) {
	return f.listFn(ctx)
}

func (f *fakeService) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	return f.domainsFn(ctx, websiteID)
}

func websiteItem(id int, domain string) ipfs.WebsiteItem {
	return ipfs.WebsiteItem{Id: id, Domain: domain}
}

func domainResp(id int, domain string) ipfs.DomainResponse {
	return ipfs.DomainResponse{Id: id, Domain: domain}
}

var errDomainList = errors.New("domain list failed")

func TestResolveWebsiteID_NumericPassthrough(t *testing.T) {
	svc := &fakeService{
		// List must not be called for a numeric argument.
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			t.Fatal("List must not be called for numeric passthrough")
			return nil, nil
		},
	}
	id, err := ResolveWebsiteID(context.Background(), svc, "42")
	require.NoError(t, err)
	assert.Equal(t, "42", id)
}

func TestResolveWebsiteID_DomainMatch(t *testing.T) {
	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{
				websiteItem(1, "example.com"),
				websiteItem(7, "Other.org."),
			}, nil
		},
	}
	// Case-insensitive + trailing-dot tolerant match via dnsname.Equal.
	id, err := ResolveWebsiteID(context.Background(), svc, "EXAMPLE.com.")
	require.NoError(t, err)
	assert.Equal(t, "1", id)
}

func TestResolveWebsiteID_NotFound(t *testing.T) {
	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{websiteItem(1, "example.com")}, nil
		},
	}
	_, err := ResolveWebsiteID(context.Background(), svc, "missing.example")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `website not found for domain "missing.example"`)
}

// pagingService is a Service fake that honors ListOptions paging so domain
// resolution demonstrably scans beyond the first page.
type pagingService struct {
	Service
	items []ipfs.WebsiteItem
}

func (p *pagingService) List(ctx context.Context, opts ListOptions) ([]ipfs.WebsiteItem, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = len(p.items)
	}
	if opts.Start >= len(p.items) {
		return nil, nil
	}
	end := opts.Start + limit
	if end > len(p.items) {
		end = len(p.items)
	}
	return p.items[opts.Start:end], nil
}

func TestResolveWebsiteID_SiteBeyondFirstPage(t *testing.T) {
	full := make([]ipfs.WebsiteItem, 25)
	for i := range full {
		full[i] = websiteItem(i+1, fmt.Sprintf("site-%d.example", i+1))
	}
	// Target sits far past the first page (defaultPageSize = 10).
	full[22] = websiteItem(23, "ivory-breeze-630.pinned.site")
	svc := &pagingService{items: full}
	id, err := ResolveWebsiteID(context.Background(), svc, "ivory-breeze-630.pinned.site")
	require.NoError(t, err)
	assert.Equal(t, "23", id)
}

func TestResolveDomainID_NameMatchWins(t *testing.T) {
	svc := &fakeService{
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			assert.Equal(t, "1", websiteID)
			return []ipfs.DomainResponse{
				domainResp(10, "www.example.com"),
				domainResp(99, "123"), // numeric-looking name must still match by name
			}, nil
		},
	}
	// "123" is a domain NAME here, not a binding ID; name matching wins.
	id, err := ResolveDomainID(context.Background(), svc, "1", "123")
	require.NoError(t, err)
	assert.Equal(t, "99", id)
}

func TestResolveDomainID_NumericMatch(t *testing.T) {
	svc := &fakeService{
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return []ipfs.DomainResponse{domainResp(10, "www.example.com")}, nil
		},
	}
	id, err := ResolveDomainID(context.Background(), svc, "1", "10")
	require.NoError(t, err)
	assert.Equal(t, "10", id)
}

func TestResolveDomainID_NotFound(t *testing.T) {
	svc := &fakeService{
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return []ipfs.DomainResponse{domainResp(10, "www.example.com")}, nil
		},
	}
	_, err := ResolveDomainID(context.Background(), svc, "1", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `domain "missing" not found for website 1`)
}

func TestResolveDomainBinding_NameMatchAcrossWebsites(t *testing.T) {
	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{websiteItem(1, "one.com"), websiteItem(2, "two.com")}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			switch websiteID {
			case "1":
				return []ipfs.DomainResponse{domainResp(11, "a.one.com")}, nil
			case "2":
				return []ipfs.DomainResponse{domainResp(22, "target.org")}, nil
			default:
				t.Fatalf("unexpected websiteID %q", websiteID)
				return nil, nil
			}
		},
	}
	wid, did, err := ResolveDomainBinding(context.Background(), svc, "target.org")
	require.NoError(t, err)
	assert.Equal(t, "2", wid)
	assert.Equal(t, "22", did)
}

func TestResolveDomainBinding_NumericIDFallback(t *testing.T) {
	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{websiteItem(1, "one.com"), websiteItem(2, "two.com")}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			switch websiteID {
			case "1":
				return []ipfs.DomainResponse{domainResp(11, "a.one.com")}, nil
			case "2":
				return []ipfs.DomainResponse{domainResp(22, "b.two.com")}, nil
			default:
				t.Fatalf("unexpected websiteID %q", websiteID)
				return nil, nil
			}
		},
	}
	// "11" is not any domain name, so the numeric binding-ID fallback wins.
	wid, did, err := ResolveDomainBinding(context.Background(), svc, "11")
	require.NoError(t, err)
	assert.Equal(t, "1", wid)
	assert.Equal(t, "11", did)
}

func TestResolveDomainBinding_NotFound(t *testing.T) {
	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{websiteItem(1, "one.com")}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return []ipfs.DomainResponse{domainResp(11, "a.one.com")}, nil
		},
	}
	_, _, err := ResolveDomainBinding(context.Background(), svc, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `domain "missing" not found bound to any website`)
}

// TestResolveDomainBinding_NameMatchWinsOverNumeric guards HNS-style namespaces
// where a domain name can itself be numeric (e.g. "123"). Even when a binding
// with numeric ID "123" exists on another website, a domain literally named
// "123" must be resolved by name, regardless of iteration order.
func TestResolveDomainBinding_NameMatchWinsOverNumeric(t *testing.T) {
	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			// Website 1 owns the numeric-ID-123 binding; website 2 owns the
			// literal domain "123". Listing order is chosen so the numeric
			// binding is seen FIRST, forcing the name-match-wins guard.
			return []ipfs.WebsiteItem{websiteItem(1, "one.com"), websiteItem(2, "two.com")}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			switch websiteID {
			case "1":
				return []ipfs.DomainResponse{domainResp(123, "a.one.com")}, nil
			case "2":
				return []ipfs.DomainResponse{domainResp(7, "123")}, nil
			default:
				t.Fatalf("unexpected websiteID %q", websiteID)
				return nil, nil
			}
		},
	}
	wid, did, err := ResolveDomainBinding(context.Background(), svc, "123")
	require.NoError(t, err)
	assert.Equal(t, "2", wid)
	assert.Equal(t, "7", did)
}

// TestResolveDomainBinding_DeferredListingErrorBlocksNumeric ensures a transient
// ListDomains failure on an unrelated website blocks the ambiguous numeric-ID
// fallback but does NOT block an unambiguous name match on another website.
func TestResolveDomainBinding_DeferredListingErrorBlocksNumeric(t *testing.T) {
	// Fallback resolution with a deferred error must error, not guess.
	svcWithErr := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{websiteItem(1, "one.com")}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return nil, errDomainList
		},
	}
	_, _, err := ResolveDomainBinding(context.Background(), svcWithErr, "11")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to look up domain on website 1")

	// Front-load the failing website, then let a clean name match succeed on
	// another website after it — the deferred error must not abort the scan.
	svcNameMatch := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{websiteItem(1, "one.com"), websiteItem(2, "two.com")}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			if websiteID == "1" {
				return nil, errDomainList
			}
			return []ipfs.DomainResponse{domainResp(22, "target.org")}, nil
		},
	}
	wid, did, err := ResolveDomainBinding(context.Background(), svcNameMatch, "target.org")
	require.NoError(t, err)
	assert.Equal(t, "2", wid)
	assert.Equal(t, "22", did)
}

// TestResolveDomainBinding_FetchesConcurrently verifies the N+1 fix: the
// per-website ListDomains calls must run concurrently rather than serially.
// The fake tracks max in-flight calls; with several websites they overlap,
// which a sequential walk would never exhibit.
func TestResolveDomainBinding_FetchesConcurrently(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0

	svc := &fakeService{
		listFn: func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{
				websiteItem(1, "a.test"),
				websiteItem(2, "b.test"),
				websiteItem(3, "c.test"),
				websiteItem(4, "d.test"),
			}, nil
		},
		domainsFn: func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()
			// Block briefly so concurrently-started calls truly overlap in the
			// in-flight window; an instantaneous handler would serialize at the
			// counter granularity and defeat the assertion.
			time.Sleep(5 * time.Millisecond)
			defer func() {
				mu.Lock()
				inFlight--
				mu.Unlock()
			}()
			// No match against the target on any website.
			return []ipfs.DomainResponse{domainResp(1, websiteID+".binding")}, nil
		},
	}

	_, _, err := ResolveDomainBinding(context.Background(), svc, "target.test")
	assert.Error(t, err, "target.test is bound nowhere")

	if maxInFlight < 2 {
		t.Errorf("ListDomains not concurrent: max in-flight = %d, want >= 2 (N+1 not collapsed)", maxInFlight)
	}
}
