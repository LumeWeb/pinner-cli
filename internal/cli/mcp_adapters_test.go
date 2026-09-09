package cli

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner/core/websites"
)

// fakeOffsetListService is a websites.Service front for only List: it serves
// the requested server-side window (queryutil _start/_end semantics: rows
// [opts.Start, opts.Start+opts.Limit) of the dataset, shorter at the end),
// exactly like the portal's website list endpoint backing
// websites.Service.List. Every List call is logged so the pagination loop's
// offset arithmetic can be asserted directly.
type fakeOffsetListService struct {
	websites.Service // only List is reached by GetByDomain; panic if anything else is hit

	dataset []ipfs.WebsiteItem

	// starts is the sequence of opts.Start values requested so far.
	starts []int
	// served is the flat multi-set of dataset indices handed out, in order.
	served []int
}

func (f *fakeOffsetListService) List(_ context.Context, opts websites.ListOptions) ([]ipfs.WebsiteItem, error) {
	f.starts = append(f.starts, opts.Start)
	end := opts.Start + opts.Limit
	if end > len(f.dataset) {
		end = len(f.dataset)
	}
	page := make([]ipfs.WebsiteItem, 0, opts.Limit)
	for i := opts.Start; i < end; i++ {
		page = append(page, f.dataset[i])
		f.served = append(f.served, i)
	}
	return page, nil
}

func domainsFor(start, end int) []ipfs.WebsiteItem {
	items := make([]ipfs.WebsiteItem, 0, end-start)
	for i := start; i < end; i++ {
		items = append(items, ipfs.WebsiteItem{Id: i + 1, Domain: fmt.Sprintf("site%d.example.com", i)})
	}
	return items
}

// TestWebsitesResourceAdapter_GetByDomainPaginatesByOffset guards the
// pagination loop in websitesResourceAdapter.GetByDomain against the
// ListOptions contract: WithPage's first argument is a 0-based ROW OFFSET into
// the full result set (not a page index), and the server short-pages the final
// window. The loop must therefore accumulate offsets by += len(items), serve
// every row exactly once (no dup/skip), and terminate at the short page.
func TestWebsitesResourceAdapter_GetByDomainPaginatesByOffset(t *testing.T) {
	// 23 items at page size 10 → pages 0-9, 10-19, 20-22 (short). Put the
	// target on the LAST page so a page-index misinterpretation (or a
	// degenerate += pageSize-1 skip) fails to find it, and a dup would break
	// the served-sequence assertion.
	const total = 23
	svc := &fakeOffsetListService{dataset: domainsFor(0, total)}
	svc.dataset[21] = ipfs.WebsiteItem{Id: 22, Domain: "target.example.com"}

	adapter := &websitesResourceAdapter{ws: svc}
	item, err := adapter.GetByDomain(context.Background(), "target.example.com")

	require.NoError(t, err)
	require.NotNil(t, item)
	require.Equal(t, "target.example.com", item.Domain)
	require.Equal(t, 22, item.Id)

	// Offset arithmetic: exactly the offsets 0, 10, 20 were requested —
	// ADVANCING, never rewinding or repeating.
	require.Equal(t, []int{0, 10, 20}, svc.starts, "offsets must advance by page size")

	// No dup / no skip: the union of served rows is exactly indices 0..22,
	// each once, in order.
	require.Len(t, svc.served, total)
	for i, got := range svc.served {
		require.Equal(t, i, got, "row %d missing/duplicated/out of order in served sequence", i)
	}
}

// TestWebsitesResourceAdapter_GetByDomainEarlyMatchStopsPaging guards that a
// match on the first page stops the scan without fetching further pages.
func TestWebsitesResourceAdapter_GetByDomainEarlyMatchStopsPaging(t *testing.T) {
	svc := &fakeOffsetListService{dataset: domainsFor(0, 23)}
	svc.dataset[3] = ipfs.WebsiteItem{Id: 4, Domain: "early.example.com"}

	adapter := &websitesResourceAdapter{ws: svc}
	item, err := adapter.GetByDomain(context.Background(), "early.example.com")

	require.NoError(t, err)
	require.NotNil(t, item)
	require.Equal(t, "early.example.com", item.Domain)
	require.Equal(t, []int{0}, svc.starts, "a first-page match must not trigger another page fetch")
	// The fake materializes the full first page (indices 0..9) before the
	// adapter stops on the match; assert no page 2 was ever requested.
	require.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, svc.served)
}

// TestWebsitesResourceAdapter_GetByDomainNotFoundTerminates guards that a
// miss over a full short-paged scan terminates (no infinite loop) and reports
// the domain-not-found error.
func TestWebsitesResourceAdapter_GetByDomainNotFoundTerminates(t *testing.T) {
	svc := &fakeOffsetListService{dataset: domainsFor(0, 23)}

	adapter := &websitesResourceAdapter{ws: svc}
	item, err := adapter.GetByDomain(context.Background(), "absent.example.com")

	require.Error(t, err)
	require.Nil(t, item)
	require.Contains(t, err.Error(), "website not found")
	// Terminated exactly at the short page: offsets 0, 10, 20 and rows 0..22.
	require.Equal(t, []int{0, 10, 20}, svc.starts)
	require.Len(t, svc.served, 23)
}
