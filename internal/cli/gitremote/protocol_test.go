package gitremote

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is a bytes-only Store stub that records the calls the protocol
// engine makes, so the engine can be tested in isolation from any real backend.
type fakeStore struct {
	refs        []Ref
	listErr     error
	downloads   []string // refName:targetHash seen
	download    io.ReadCloser
	downloadErr error
	uploads     []uploadCall
	uploadErr   error
	deletes     []string
	deleteErr   error
}

type uploadCall struct {
	dstRef  string
	srcHash string
	pack    []byte
}

func (f *fakeStore) List(_ context.Context, _ bool) ([]Ref, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.refs, nil
}

func (f *fakeStore) Download(_ context.Context, refName, targetHash string) (io.ReadCloser, error) {
	f.downloads = append(f.downloads, refName+":"+targetHash)
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	if f.download != nil {
		return f.download, nil
	}
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (f *fakeStore) Upload(_ context.Context, dstRef, srcHash string, pack io.Reader) error {
	data, _ := io.ReadAll(pack)
	f.uploads = append(f.uploads, uploadCall{dstRef: dstRef, srcHash: srcHash, pack: data})
	return f.uploadErr
}

func (f *fakeStore) Delete(_ context.Context, dstRef string) error {
	f.deletes = append(f.deletes, dstRef)
	return f.deleteErr
}

func (f *fakeStore) Close() error { return nil }

// handleText runs Handle against an empty in-memory local store with a synthetic
// stdin string and returns the stdout text. Protocol tests only assert the
// command/output wiring; object movement is covered by the go-git integration
// test.
func handleText(t *testing.T, store Store, in string) string {
	t.Helper()
	var out, errw bytes.Buffer
	err := Handle(context.Background(), memory.NewStorage(), store, strings.NewReader(in), &out, &errw)
	require.NoError(t, err, "Handle failed: %v", err)
	return out.String()
}

func TestCapabilities(t *testing.T) {
	out := handleText(t, &fakeStore{}, "capabilities\n")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Equal(t, []string{"option", "fetch", "push", ""}, lines)
}

func TestListAdvertisesRefs(t *testing.T) {
	store := &fakeStore{refs: []Ref{
		{Name: "refs/heads/master", Hash: plumbing.NewHash("aaaabbbbccccddddeeeeffff0000111122223333")},
		{Name: "refs/tags/v1", Hash: plumbing.NewHash("1111222233334444555566667777888899990000")},
	}}
	out := handleText(t, store, "capabilities\nlist\n")
	assert.Contains(t, out, "aaaabbbbccccddddeeeeffff0000111122223333 refs/heads/master\n")
	assert.Contains(t, out, "1111222233334444555566667777888899990000 refs/tags/v1\n")
	assert.True(t, strings.HasSuffix(out, "\n\n"))
}

// TestListForPush advertises refs in `list for-push` mode (the preparer for a
// push batch). The old-tip-as-haves incremental path is exercised with real
// objects in TestGoGitPushThenFetch (integration test).
func TestListForPush(t *testing.T) {
	store := &fakeStore{refs: []Ref{
		{Name: "refs/heads/master", Hash: plumbing.NewHash("bbb0000000000000000000000000000000000000")},
	}}
	out := handleText(t, store, "capabilities\nlist for-push\n")
	assert.Contains(t, out, "bbb0000000000000000000000000000000000000 refs/heads/master\n")
}

func TestPushOkAndDelete(t *testing.T) {
	store := &fakeStore{}
	const in = "" +
		"capabilities\n" +
		"list for-push\n" +
		"push :refs/heads/gone\n" +
		"\n"
	out := handleText(t, store, in)
	require.Equal(t, []string{"refs/heads/gone"}, store.deletes)
	assert.Contains(t, out, "ok refs/heads/gone\n")
	assert.True(t, strings.HasSuffix(out, "\n\n"))
}

func TestOptionHandling(t *testing.T) {
	store := &fakeStore{}
	const in = "" +
		"capabilities\n" +
		"option verbosity 3\n" +
		"option object-format true\n" +
		"option not-a-real-option 1\n" +
		"list\n"
	out := handleText(t, store, in)
	assert.Contains(t, out, "ok\n")
	assert.Contains(t, out, "unsupported\n")
}

func TestFetchBatchOutputsBlankLine(t *testing.T) {
	store := &fakeStore{download: io.NopCloser(bytes.NewReader(nil))}
	const in = "" +
		"capabilities\n" +
		"list\n" +
		"fetch aaaabbbbccccddddeeeeffff0000111122223333 refs/heads/master\n" +
		"fetch 1111222233334444555566667777888899990000 refs/heads/dev\n" +
		"\n"
	out := handleText(t, store, in)
	require.Equal(t, []string{
		"refs/heads/master:aaaabbbbccccddddeeeeffff0000111122223333",
		"refs/heads/dev:1111222233334444555566667777888899990000",
	}, store.downloads)
	// Output: capabilities response, empty list, then exactly one blank line
	// terminating the fetch batch.
	assert.Equal(t, "option\nfetch\npush\n\n\n\n", out)
}

func TestFetchFails(t *testing.T) {
	store := &fakeStore{downloadErr: io.ErrUnexpectedEOF}
	const in = "" +
		"capabilities\n" +
		"list\n" +
		"fetch aaaabbbbccccddddeeeeffff0000111122223333 refs/heads/master\n" +
		"\n"
	var out, errw bytes.Buffer
	err := Handle(context.Background(), memory.NewStorage(), store, strings.NewReader(in), &out, &errw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch")
}

func TestPushFailureReportedPerRef(t *testing.T) {
	store := &fakeStore{uploadErr: io.ErrClosedPipe}
	const in = "" +
		"capabilities\n" +
		"list for-push\n" +
		"push ccc1111111111111111111111111111111111111:refs/heads/master\n" +
		"push :refs/heads/other\n" +
		"\n"
	out := handleText(t, store, in)
	// Push errors are per-ref status lines, not fatal stream errors.
	assert.Contains(t, out, "error refs/heads/master ")
	assert.Contains(t, out, "ok refs/heads/other\n")
	assert.True(t, strings.HasSuffix(out, "\n\n"))
}

func TestUnknownCommandFatal(t *testing.T) {
	var out bytes.Buffer
	err := Handle(context.Background(), memory.NewStorage(), &fakeStore{}, strings.NewReader("capabilities\nbogus\n"), &out, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}
