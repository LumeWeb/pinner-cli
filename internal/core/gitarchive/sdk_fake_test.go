package gitarchive_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
)

// fakeSDK is an in-memory SDKClient used to exercise the git archive
// upload/download plumbing without a live indexer. Uploads are stored by their
// content-addressed object key; Object/Download read them back. It also
// records the sequence of pins for assertions.
type fakeSDK struct {
	mu      sync.Mutex
	objects map[types.Hash256]fakeStored
	pinned  []types.Hash256

	// shared-object stubs (used by the share-URL path).
	shareURL   string
	shareErr   error
	sharedData []byte
	// mintURLs, when set, overrides the returned pre-signed URL per object key.
	mintURLs map[types.Hash256]string
	// mintLog records, in order, every key handed to CreateSharedObjectURL.
	mintLog []types.Hash256
	// sharedByURL, when set, serves DownloadSharedObject payloads per URL.
	sharedByURL map[string][]byte
}

type fakeStored struct {
	meta json.RawMessage
	data []byte
}

func newFakeSDK() *fakeSDK {
	return &fakeSDK{objects: map[types.Hash256]fakeStored{}}
}

func (f *fakeSDK) Upload(ctx context.Context, obj *siastorage.Object, r io.Reader, _ ...siastorage.UploadOption) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[obj.ID()] = fakeStored{meta: obj.Metadata(), data: data}
	return nil
}

func (f *fakeSDK) PinObject(ctx context.Context, obj siastorage.Object) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pinned = append(f.pinned, obj.ID())
	return nil
}

func (f *fakeSDK) Object(ctx context.Context, key types.Hash256) (siastorage.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fs, ok := f.objects[key]
	if !ok {
		return siastorage.Object{}, fmt.Errorf("fake: object %s not found", key)
	}
	obj := siastorage.NewEmptyObject()
	obj.UpdateMetadata(fs.meta)
	return obj, nil
}

func (f *fakeSDK) Download(obj siastorage.Object, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fs, ok := f.objects[obj.ID()]
	if !ok {
		return nil, fmt.Errorf("fake: no data for object %s", obj.ID())
	}
	return io.NopCloser(bytes.NewReader(fs.data)), nil
}

func (f *fakeSDK) CreateSharedObjectURL(ctx context.Context, objectKey types.Hash256, validUntil time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mintLog = append(f.mintLog, objectKey)
	if f.shareErr != nil {
		return "", f.shareErr
	}
	if u, ok := f.mintURLs[objectKey]; ok {
		return u, nil
	}
	if f.shareURL != "" {
		return f.shareURL, nil
	}
	// No explicit URL set: derive a stable, key-specific URL so multiple objects
	// mint distinct URLs without the caller pre-populating the map.
	return "_fake_share_" + objectKey.String(), nil
}

func (f *fakeSDK) DownloadSharedObject(ctx context.Context, sharedURL string, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.shareErr != nil {
		return nil, f.shareErr
	}
	if data, ok := f.sharedByURL[sharedURL]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	if f.sharedData == nil {
		return nil, fmt.Errorf("fake: no shared object data")
	}
	return io.NopCloser(bytes.NewReader(f.sharedData)), nil
}

func (f *fakeSDK) Close() error { return nil }
