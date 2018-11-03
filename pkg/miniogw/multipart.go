// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package miniogw

import (
	"context"
	"io"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	minio "github.com/minio/minio/cmd"
	"github.com/minio/minio/pkg/hash"

	"storj.io/storj/pkg/storage/objects"
)

func (s *storjObjects) NewMultipartUpload(ctx context.Context, bucket, object string, metadata map[string]string) (uploadID string, err error) {
	defer mon.Task()(&ctx)(&err)

	uploads := s.storj.multipart

	upload, err := uploads.Create(bucket, object, metadata)
	if err != nil {
		return "", err
	}

	objectStore, err := s.storj.bs.GetObjectStore(ctx, bucket)
	if err != nil {
		uploads.RemoveByID(upload.ID)
		upload.fail(err)
		return "", err
	}

	go func() {
		// setting zero value means the object never expires
		expTime := time.Time{}

		tempContType := metadata["content-type"]
		delete(metadata, "content-type")

		//metadata serialized
		serMetaInfo := objects.SerializableMeta{
			ContentType: tempContType,
			UserDefined: metadata,
		}

		result, err := objectStore.Put(ctx, object, upload.Stream, serMetaInfo, expTime)
		uploads.RemoveByID(upload.ID)

		if err != nil {
			upload.fail(err)
		} else {
			upload.complete(minio.ObjectInfo{
				Name:        object,
				Bucket:      bucket,
				ModTime:     result.Modified,
				Size:        result.Size,
				ETag:        result.Checksum,
				ContentType: result.ContentType,
				UserDefined: result.UserDefined,
			})
		}
	}()

	return upload.ID, nil
}

func (s *storjObjects) PutObjectPart(ctx context.Context, bucket, object, uploadID string, partID int, data *hash.Reader) (info minio.PartInfo, err error) {
	defer mon.Task()(&ctx)(&err)

	uploads := s.storj.multipart

	upload, err := uploads.Get(bucket, object, uploadID)
	if err != nil {
		return minio.PartInfo{}, err
	}

	part, err := upload.Stream.AddPart(partID, data)
	if err != nil {
		return minio.PartInfo{}, err
	}

	err = <-part.Done
	if err != nil {
		return minio.PartInfo{}, err
	}

	partInfo := minio.PartInfo{
		PartNumber:   part.Number,
		LastModified: time.Now(),
		ETag:         data.SHA256HexString(),
		Size:         atomic.LoadInt64(&part.Size),
	}

	upload.addCompletedPart(partInfo)

	return partInfo, nil
}

func (s *storjObjects) AbortMultipartUpload(ctx context.Context, bucket, object, uploadID string) (err error) {
	defer mon.Task()(&ctx)(&err)

	uploads := s.storj.multipart

	upload, err := uploads.Remove(bucket, object, uploadID)
	if err != nil {
		return err
	}

	errAbort := Error.New("abort")
	upload.Stream.Abort(errAbort)
	r := <-upload.Done
	if r.Error != errAbort {
		return r.Error
	}
	return nil
}

func (s *storjObjects) CompleteMultipartUpload(ctx context.Context, bucket, object, uploadID string, uploadedParts []minio.CompletePart) (objInfo minio.ObjectInfo, err error) {
	defer mon.Task()(&ctx)(&err)

	uploads := s.storj.multipart
	upload, err := uploads.Remove(bucket, object, uploadID)
	if err != nil {
		return minio.ObjectInfo{}, err
	}

	// notify stream that there aren't more parts coming
	upload.Stream.Close()
	// wait for completion
	result := <-upload.Done
	// return the final info
	return result.Info, result.Error
}

func (s *storjObjects) ListObjectParts(ctx context.Context, bucket, object, uploadID string, partNumberMarker int, maxParts int) (result minio.ListPartsInfo, err error) {
	uploads := s.storj.multipart
	upload, err := uploads.Get(bucket, object, uploadID)
	if err != nil {
		return minio.ListPartsInfo{}, err
	}

	list := minio.ListPartsInfo{}

	list.Bucket = bucket
	list.Object = object
	list.UploadID = uploadID
	list.PartNumberMarker = partNumberMarker
	list.MaxParts = maxParts
	list.UserDefined = upload.Metadata
	list.Parts = upload.getCompletedParts()

	sort.Slice(list.Parts, func(i, k int) bool {
		return list.Parts[i].PartNumber < list.Parts[k].PartNumber
	})

	var first int
	for i, p := range list.Parts {
		first = i
		if partNumberMarker <= p.PartNumber {
			break
		}
	}

	list.Parts = list.Parts[first:]
	if len(list.Parts) > maxParts {
		list.NextPartNumberMarker = list.Parts[maxParts].PartNumber
		list.Parts = list.Parts[:maxParts]
		list.IsTruncated = true
	}

	return list, nil
}

// TODO: implement
// func (s *storjObjects) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker, delimiter string, maxUploads int) (result minio.ListMultipartsInfo, err error) {
// func (s *storjObjects) CopyObjectPart(ctx context.Context, srcBucket, srcObject, destBucket, destObject string, uploadID string, partID int, startOffset int64, length int64, srcInfo minio.ObjectInfo) (info minio.PartInfo, err error) {

// MultipartUploads manages pending multipart uploads
type MultipartUploads struct {
	mu      sync.RWMutex
	lastID  int
	pending map[string]*MultipartUpload
}

// NewMultipartUploads creates new MultipartUploads
func NewMultipartUploads() *MultipartUploads {
	return &MultipartUploads{
		pending: map[string]*MultipartUpload{},
	}
}

// Create creates a new upload
func (uploads *MultipartUploads) Create(bucket, object string, metadata map[string]string) (*MultipartUpload, error) {
	uploads.mu.Lock()
	defer uploads.mu.Unlock()

	for id, upload := range uploads.pending {
		if upload.Bucket == bucket && upload.Object == object {
			upload.Stream.Abort(Error.New("aborted by another upload to the same location"))
			delete(uploads.pending, id)
		}
	}

	uploads.lastID++
	uploadID := "Upload" + strconv.Itoa(uploads.lastID)

	upload := NewMultipartUpload(uploadID, bucket, object, metadata)
	uploads.pending[uploadID] = upload

	return upload, nil
}

// Get finds a pending upload
func (uploads *MultipartUploads) Get(bucket, object, uploadID string) (*MultipartUpload, error) {
	uploads.mu.Lock()
	defer uploads.mu.Unlock()

	upload, ok := uploads.pending[uploadID]
	if !ok {
		return nil, Error.New("pending upload %q missing", uploadID)
	}
	if upload.Bucket != bucket || upload.Object != object {
		return nil, Error.New("pending upload %q bucket/object name mismatch", uploadID)
	}

	return upload, nil
}

// Remove returns and removes a pending upload
func (uploads *MultipartUploads) Remove(bucket, object, uploadID string) (*MultipartUpload, error) {
	uploads.mu.RLock()
	defer uploads.mu.RUnlock()

	upload, ok := uploads.pending[uploadID]
	if !ok {
		return nil, Error.New("pending upload %q missing", uploadID)
	}
	if upload.Bucket != bucket || upload.Object != object {
		return nil, Error.New("pending upload %q bucket/object name mismatch", uploadID)
	}

	delete(uploads.pending, uploadID)

	return upload, nil
}

// RemoveByID removes pending upload by id
func (uploads *MultipartUploads) RemoveByID(uploadID string) {
	uploads.mu.RLock()
	defer uploads.mu.RUnlock()
	delete(uploads.pending, uploadID)
}

// MultipartUpload is partial info about a pending upload
type MultipartUpload struct {
	ID       string
	Bucket   string
	Object   string
	Metadata map[string]string
	Done     chan (*MultipartUploadResult)
	Stream   *MultipartStream

	mu        sync.Mutex
	completed []minio.PartInfo
}

// MultipartUploadResult contains either an Error or the uploaded ObjectInfo
type MultipartUploadResult struct {
	Error error
	Info  minio.ObjectInfo
}

// NewMultipartUpload creates a new MultipartUpload
func NewMultipartUpload(uploadID string, bucket, object string, metadata map[string]string) *MultipartUpload {
	upload := &MultipartUpload{
		ID:       uploadID,
		Bucket:   bucket,
		Object:   object,
		Metadata: metadata,
		Done:     make(chan *MultipartUploadResult, 1),
		Stream:   NewMultipartStream(),
	}
	return upload
}

// addCompletedPart adds a completed part to the list
func (upload *MultipartUpload) addCompletedPart(part minio.PartInfo) {
	upload.mu.Lock()
	defer upload.mu.Unlock()

	upload.completed = append(upload.completed, part)
}

func (upload *MultipartUpload) getCompletedParts() []minio.PartInfo {
	upload.mu.Lock()
	defer upload.mu.Unlock()

	return append([]minio.PartInfo{}, upload.completed...)
}

// fail aborts the upload with an error
func (upload *MultipartUpload) fail(err error) {
	upload.Done <- &MultipartUploadResult{Error: err}
	close(upload.Done)
}

// complete completes the upload
func (upload *MultipartUpload) complete(info minio.ObjectInfo) {
	upload.Done <- &MultipartUploadResult{Info: info}
	close(upload.Done)
}

// MultipartStream serializes multiple readers into a single reader
type MultipartStream struct {
	mu         sync.Mutex
	moreParts  sync.Cond
	err        error
	closed     bool
	finished   bool
	nextID     int
	nextNumber int
	parts      []*StreamPart
}

// StreamPart is a reader waiting in MultipartStream
type StreamPart struct {
	Number int
	ID     int
	Size   int64
	Reader *hash.Reader
	Done   chan error
}

// NewMultipartStream creates a new MultipartStream
func NewMultipartStream() *MultipartStream {
	stream := &MultipartStream{}
	stream.moreParts.L = &stream.mu
	stream.nextID = 1
	return stream
}

// Abort aborts the stream reading
func (stream *MultipartStream) Abort(err error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()

	if stream.finished {
		return
	}

	if stream.err == nil {
		stream.err = err
	}
	stream.finished = true
	stream.closed = true

	for _, part := range stream.parts {
		part.Done <- err
		close(part.Done)
	}
	stream.parts = nil

	stream.moreParts.Broadcast()
}

// Close closes the stream, but lets it complete
func (stream *MultipartStream) Close() {
	stream.mu.Lock()
	defer stream.mu.Unlock()

	stream.closed = true
	stream.moreParts.Broadcast()
}

// Read implements io.Reader interface, blocking when there's no part
func (stream *MultipartStream) Read(data []byte) (n int, err error) {
	var part *StreamPart
	stream.mu.Lock()
	for {
		// has an error occurred?
		if stream.err != nil {
			stream.mu.Unlock()
			return 0, Error.Wrap(err)
		}
		// do we have the next part?
		if len(stream.parts) > 0 && stream.nextID == stream.parts[0].ID {
			part = stream.parts[0]
			break
		}
		// we don't have the next part and are closed, hence we are complete
		if stream.closed {
			stream.finished = true
			stream.mu.Unlock()
			return 0, io.EOF
		}

		stream.moreParts.Wait()
	}
	stream.mu.Unlock()

	// read as much as we can
	n, err = part.Reader.Read(data)
	atomic.AddInt64(&part.Size, int64(n))

	if err == io.EOF {
		// the part completed, hence advance to the next one
		err = nil

		stream.mu.Lock()
		stream.parts = stream.parts[1:]
		stream.nextID++
		stream.mu.Unlock()

		close(part.Done)
	} else if err != nil {
		// something bad happened, abort the whole thing
		stream.Abort(err)
		return n, Error.Wrap(err)
	}

	return n, err
}

// AddPart adds a new part to the stream to wait
func (stream *MultipartStream) AddPart(partID int, data *hash.Reader) (*StreamPart, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()

	for _, p := range stream.parts {
		if p.ID == partID {
			return nil, Error.New("Part %d already exists", partID)
		}
	}

	stream.nextNumber++
	part := &StreamPart{
		Number: stream.nextNumber - 1,
		ID:     partID,
		Size:   0,
		Reader: data,
		Done:   make(chan error, 1),
	}

	stream.parts = append(stream.parts, part)
	sort.Slice(stream.parts, func(i, k int) bool {
		return stream.parts[i].ID < stream.parts[k].ID
	})

	stream.moreParts.Broadcast()

	return part, nil
}
