// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package miniogw

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	minio "github.com/minio/minio/cmd"
	"github.com/minio/minio/pkg/auth"
	"github.com/minio/minio/pkg/hash"
	"github.com/stretchr/testify/assert"
	"github.com/vivint/infectious"

	"storj.io/storj/internal/memory"
	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
	"storj.io/storj/pkg/eestream"
	"storj.io/storj/pkg/metainfo/kvmetainfo"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/storage/buckets"
	"storj.io/storj/pkg/storage/ec"
	"storj.io/storj/pkg/storage/segments"
	"storj.io/storj/pkg/storage/streams"
	"storj.io/storj/pkg/storj"
)

const (
	TestAPIKey = "test-api-key"
	TestEncKey = "test-encryption-key"
	TestBucket = "test-bucket"
	TestFile   = "test-file"
	DestBucket = "dest-bucket"
	DestFile   = "dest-file"
)

func TestMakeBucketWithLocation(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when creating bucket with empty name
		err := layer.MakeBucketWithLocation(ctx, "", "")
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Create a bucket with the Minio API
		err = layer.MakeBucketWithLocation(ctx, TestBucket, "")
		assert.NoError(t, err)

		// Check that the bucket is created using the Metainfo API
		bucket, err := metainfo.GetBucket(ctx, TestBucket)
		assert.NoError(t, err)
		assert.Equal(t, TestBucket, bucket.Name)
		assert.True(t, time.Since(bucket.Created) < 1*time.Second)
		assert.Equal(t, storj.AESGCM, bucket.PathCipher)

		// Check the error when trying to create an existing bucket
		err = layer.MakeBucketWithLocation(ctx, TestBucket, "")
		assert.Equal(t, minio.BucketAlreadyExists{Bucket: TestBucket}, err)
	})
}

func TestGetBucketInfo(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when getting info about bucket with empty name
		_, err := layer.GetBucketInfo(ctx, "")
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when getting info about non-existing bucket
		_, err = layer.GetBucketInfo(ctx, TestBucket)
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the bucket using the Metainfo API
		info, err := metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		// Check the bucket info using the Minio API
		bucket, err := layer.GetBucketInfo(ctx, TestBucket)
		if assert.NoError(t, err) {
			assert.Equal(t, TestBucket, bucket.Name)
			assert.Equal(t, info.Created, bucket.Created)
		}
	})
}

func TestDeleteBucket(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when deleting bucket with empty name
		err := layer.DeleteBucket(ctx, "")
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when deleting non-existing bucket
		err = layer.DeleteBucket(ctx, TestBucket)
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create a bucket with a file using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		_, err = createFile(ctx, metainfo, streams, TestBucket, TestFile, nil, nil)
		assert.NoError(t, err)

		// Check the error when deleting non-empty bucket
		err = layer.DeleteBucket(ctx, TestBucket)
		assert.Equal(t, minio.BucketNotEmpty{Bucket: TestBucket}, err)

		// Delete the file using the Metainfo API, so the bucket becomes empty
		err = metainfo.DeleteObject(ctx, TestBucket, TestFile)
		assert.NoError(t, err)

		// Delete the bucket info using the Minio API
		err = layer.DeleteBucket(ctx, TestBucket)
		assert.NoError(t, err)

		// Check that the bucket is deleted using the Metainfo API
		_, err = metainfo.GetBucket(ctx, TestBucket)
		assert.True(t, storj.ErrBucketNotFound.Has(err))
	})
}

func TestListBuckets(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check that empty list is return if no buckets exist yet
		bucketInfos, err := layer.ListBuckets(ctx)
		assert.NoError(t, err)
		assert.Empty(t, bucketInfos)

		// Create all expected buckets using the Metainfo API
		bucketNames := []string{"bucket 1", "bucket 2", "bucket 3"}
		buckets := make([]storj.Bucket, len(bucketNames))
		for i, bucketName := range bucketNames {
			bucket, err := metainfo.CreateBucket(ctx, bucketName, nil)
			buckets[i] = bucket
			assert.NoError(t, err)
		}

		// Check that the expected buckets can be listed using the Minio API
		bucketInfos, err = layer.ListBuckets(ctx)
		if assert.NoError(t, err) {
			assert.Equal(t, len(bucketNames), len(bucketInfos))
			for i, bucketInfo := range bucketInfos {
				assert.Equal(t, bucketNames[i], bucketInfo.Name)
				assert.Equal(t, buckets[i].Created, bucketInfo.Created)
			}
		}
	})
}

func TestPutObject(t *testing.T) {
	data, err := hash.NewReader(bytes.NewReader([]byte("test")),
		int64(len("test")),
		"098f6bcd4621d373cade4e832627b4f6",
		"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
	if err != nil {
		t.Fatal(err)
	}

	metadata := map[string]string{
		"content-type": "media/foo",
		"key1":         "value1",
		"key2":         "value2",
	}

	serMetaInfo := pb.SerializableMeta{
		ContentType: metadata["content-type"],
		UserDefined: map[string]string{
			"key1": metadata["key1"],
			"key2": metadata["key2"],
		},
	}

	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when putting an object to a bucket with empty name
		_, err := layer.PutObject(ctx, "", "", nil, nil)
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when putting an object to a non-existing bucket
		_, err = layer.PutObject(ctx, TestBucket, TestFile, nil, nil)
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the bucket using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		// Check the error when putting an object with empty name
		_, err = layer.PutObject(ctx, TestBucket, "", nil, nil)
		assert.Equal(t, minio.ObjectNameInvalid{Bucket: TestBucket}, err)

		// Put the object using the Minio API
		info, err := layer.PutObject(ctx, TestBucket, TestFile, data, metadata)
		if assert.NoError(t, err) {
			assert.Equal(t, TestFile, info.Name)
			assert.Equal(t, TestBucket, info.Bucket)
			assert.False(t, info.IsDir)
			assert.True(t, time.Since(info.ModTime) < 1*time.Second)
			assert.Equal(t, data.Size(), info.Size)
			// assert.Equal(t, data.SHA256HexString(), info.ETag) TODO: when we start calculating checksums
			assert.Equal(t, serMetaInfo.ContentType, info.ContentType)
			assert.Equal(t, serMetaInfo.UserDefined, info.UserDefined)
		}

		// Check that the object is uploaded using the Metainfo API
		obj, err := metainfo.GetObject(ctx, TestBucket, TestFile)
		if assert.NoError(t, err) {
			assert.Equal(t, TestFile, obj.Path)
			assert.Equal(t, TestBucket, obj.Bucket.Name)
			assert.False(t, obj.IsPrefix)
			assert.Equal(t, info.ModTime, obj.Modified)
			assert.Equal(t, info.Size, obj.Size)
			assert.Equal(t, info.ETag, hex.EncodeToString(obj.Checksum))
			assert.Equal(t, info.ContentType, obj.ContentType)
			assert.Equal(t, info.UserDefined, obj.Metadata)
		}
	})
}

func TestGetObjectInfo(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when getting an object from a bucket with empty name
		_, err := layer.GetObjectInfo(ctx, "", "")
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when getting an object from non-existing bucket
		_, err = layer.GetObjectInfo(ctx, TestBucket, TestFile)
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the bucket using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		// Check the error when getting an object with empty name
		_, err = layer.GetObjectInfo(ctx, TestBucket, "")
		assert.Equal(t, minio.ObjectNameInvalid{Bucket: TestBucket}, err)

		// Check the error when getting a non-existing object
		_, err = layer.GetObjectInfo(ctx, TestBucket, TestFile)
		assert.Equal(t, minio.ObjectNotFound{Bucket: TestBucket, Object: TestFile}, err)

		// Create the object using the Metainfo API
		createInfo := storj.CreateObject{
			ContentType: "text/plain",
			Metadata:    map[string]string{"key1": "value1", "key2": "value2"},
		}
		obj, err := createFile(ctx, metainfo, streams, TestBucket, TestFile, &createInfo, []byte("test"))
		assert.NoError(t, err)

		// Get the object info using the Minio API
		info, err := layer.GetObjectInfo(ctx, TestBucket, TestFile)
		if assert.NoError(t, err) {
			assert.Equal(t, TestFile, info.Name)
			assert.Equal(t, TestBucket, info.Bucket)
			assert.False(t, info.IsDir)
			assert.Equal(t, obj.Modified, info.ModTime)
			assert.Equal(t, obj.Size, info.Size)
			assert.Equal(t, hex.EncodeToString(obj.Checksum), info.ETag)
			assert.Equal(t, createInfo.ContentType, info.ContentType)
			assert.Equal(t, createInfo.Metadata, info.UserDefined)
		}
	})
}

func TestGetObject(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when getting an object from a bucket with empty name
		err := layer.GetObject(ctx, "", "", 0, 0, nil, "")
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when getting an object from non-existing bucket
		err = layer.GetObject(ctx, TestBucket, TestFile, 0, 0, nil, "")
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the bucket using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		// Check the error when getting an object with empty name
		err = layer.GetObject(ctx, TestBucket, "", 0, 0, nil, "")
		assert.Equal(t, minio.ObjectNameInvalid{Bucket: TestBucket}, err)

		// Check the error when getting a non-existing object
		err = layer.GetObject(ctx, TestBucket, TestFile, 0, 0, nil, "")
		assert.Equal(t, minio.ObjectNotFound{Bucket: TestBucket, Object: TestFile}, err)

		// Create the object using the Metainfo API
		createInfo := storj.CreateObject{
			ContentType: "text/plain",
			Metadata:    map[string]string{"key1": "value1", "key2": "value2"},
		}
		_, err = createFile(ctx, metainfo, streams, TestBucket, TestFile, &createInfo, []byte("abcdef"))
		assert.NoError(t, err)

		for i, tt := range []struct {
			offset, length int64
			substr         string
			err            error
		}{
			{offset: 0, length: 0, substr: ""},
			{offset: 3, length: 0, substr: ""},
			{offset: 0, length: -1, substr: "abcdef"},
			{offset: 0, length: 6, substr: "abcdef"},
			{offset: 0, length: 5, substr: "abcde"},
			{offset: 0, length: 4, substr: "abcd"},
			{offset: 1, length: 4, substr: "bcde"},
			{offset: 2, length: 4, substr: "cdef"},
			{offset: 0, length: 7, substr: "", err: minio.InvalidRange{OffsetBegin: 0, OffsetEnd: 7, ResourceSize: 6}},
			{offset: -1, length: 7, substr: "", err: minio.InvalidRange{OffsetBegin: -1, OffsetEnd: 6, ResourceSize: 6}},
			{offset: 0, length: -2, substr: "", err: minio.InvalidRange{OffsetBegin: 0, OffsetEnd: -2, ResourceSize: 6}},
		} {
			errTag := fmt.Sprintf("%d. %+v", i, tt)

			var buf bytes.Buffer

			// Get the object info using the Minio API
			err = layer.GetObject(ctx, TestBucket, TestFile, tt.offset, tt.length, &buf, "")

			if tt.err != nil {
				assert.Equal(t, tt.err, err, errTag)
			} else if assert.NoError(t, err) {
				assert.Equal(t, tt.substr, buf.String(), errTag)
			}
		}
	})
}

func TestCopyObject(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when copying an object from a bucket with empty name
		_, err := layer.CopyObject(ctx, "", TestFile, DestBucket, DestFile, minio.ObjectInfo{})
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when copying an object from non-existing bucket
		_, err = layer.CopyObject(ctx, TestBucket, TestFile, DestBucket, DestFile, minio.ObjectInfo{})
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the source bucket using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		// Check the error when copying an object with empty name
		_, err = layer.CopyObject(ctx, TestBucket, "", DestBucket, DestFile, minio.ObjectInfo{})
		assert.Equal(t, minio.ObjectNameInvalid{Bucket: TestBucket}, err)

		// Create the source object using the Metainfo API
		createInfo := storj.CreateObject{
			ContentType: "text/plain",
			Metadata:    map[string]string{"key1": "value1", "key2": "value2"},
		}
		obj, err := createFile(ctx, metainfo, streams, TestBucket, TestFile, &createInfo, []byte("test"))
		assert.NoError(t, err)

		// Get the source object info using the Minio API
		srcInfo, err := layer.GetObjectInfo(ctx, TestBucket, TestFile)
		assert.NoError(t, err)

		// Check the error when copying an object to a bucket with empty name
		_, err = layer.CopyObject(ctx, TestBucket, TestFile, "", DestFile, srcInfo)
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when copying an object to a non-existing bucket
		_, err = layer.CopyObject(ctx, TestBucket, TestFile, DestBucket, DestFile, srcInfo)
		assert.Equal(t, minio.BucketNotFound{Bucket: DestBucket}, err)

		// Create the destination bucket using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, DestBucket, nil)
		assert.NoError(t, err)

		// Copy the object using the Minio API
		info, err := layer.CopyObject(ctx, TestBucket, TestFile, DestBucket, DestFile, srcInfo)
		if assert.NoError(t, err) {
			assert.Equal(t, DestFile, info.Name)
			assert.Equal(t, DestBucket, info.Bucket)
			assert.False(t, info.IsDir)
			assert.True(t, info.ModTime.Sub(obj.Modified) < 1*time.Second)
			assert.Equal(t, obj.Size, info.Size)
			assert.Equal(t, hex.EncodeToString(obj.Checksum), info.ETag)
			assert.Equal(t, createInfo.ContentType, info.ContentType)
			assert.Equal(t, createInfo.Metadata, info.UserDefined)
		}

		// Check that the destination object is uploaded using the Metainfo API
		obj, err = metainfo.GetObject(ctx, DestBucket, DestFile)
		if assert.NoError(t, err) {
			assert.Equal(t, DestFile, obj.Path)
			assert.Equal(t, DestBucket, obj.Bucket.Name)
			assert.False(t, obj.IsPrefix)
			assert.Equal(t, info.ModTime, obj.Modified)
			assert.Equal(t, info.Size, obj.Size)
			assert.Equal(t, info.ETag, hex.EncodeToString(obj.Checksum))
			assert.Equal(t, info.ContentType, obj.ContentType)
			assert.Equal(t, info.UserDefined, obj.Metadata)
		}
	})
}

func TestDeleteObject(t *testing.T) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when deleting an object from a bucket with empty name
		err := layer.DeleteObject(ctx, "", "")
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when deleting an object from non-existing bucket
		err = layer.DeleteObject(ctx, TestBucket, TestFile)
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the bucket using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, nil)
		assert.NoError(t, err)

		// Check the error when deleting an object with empty name
		err = layer.DeleteObject(ctx, TestBucket, "")
		assert.Equal(t, minio.ObjectNameInvalid{Bucket: TestBucket}, err)

		// Check the error when deleting a non-existing object
		err = layer.DeleteObject(ctx, TestBucket, TestFile)
		assert.Equal(t, minio.ObjectNotFound{Bucket: TestBucket, Object: TestFile}, err)

		// Create the object using the Metainfo API
		_, err = createFile(ctx, metainfo, streams, TestBucket, TestFile, nil, nil)
		assert.NoError(t, err)

		// Delete the object info using the Minio API
		err = layer.DeleteObject(ctx, TestBucket, TestFile)
		assert.NoError(t, err)

		// Check that the object is deleted using the Metainfo API
		_, err = metainfo.GetObject(ctx, TestBucket, TestFile)
		assert.True(t, storj.ErrObjectNotFound.Has(err))
	})
}

func TestListObjects(t *testing.T) {
	testListObjects(t, func(ctx context.Context, layer minio.ObjectLayer, bucket, prefix, marker, delimiter string, maxKeys int) ([]string, []minio.ObjectInfo, bool, error) {
		list, err := layer.ListObjects(ctx, TestBucket, prefix, marker, delimiter, maxKeys)
		if err != nil {
			return nil, nil, false, err
		}
		return list.Prefixes, list.Objects, list.IsTruncated, nil
	})
}

func TestListObjectsV2(t *testing.T) {
	testListObjects(t, func(ctx context.Context, layer minio.ObjectLayer, bucket, prefix, marker, delimiter string, maxKeys int) ([]string, []minio.ObjectInfo, bool, error) {
		list, err := layer.ListObjectsV2(ctx, TestBucket, prefix, marker, delimiter, maxKeys, false, "")
		if err != nil {
			return nil, nil, false, err
		}
		return list.Prefixes, list.Objects, list.IsTruncated, nil
	})
}

func testListObjects(t *testing.T, listObjects func(context.Context, minio.ObjectLayer, string, string, string, string, int) ([]string, []minio.ObjectInfo, bool, error)) {
	runTest(t, func(ctx context.Context, layer minio.ObjectLayer, metainfo storj.Metainfo, streams streams.Store) {
		// Check the error when listing objects with unsupported delimiter
		_, err := layer.ListObjects(ctx, TestBucket, "", "", "#", 0)
		assert.Equal(t, minio.UnsupportedDelimiter{Delimiter: "#"}, err)

		// Check the error when listing objects in a bucket with empty name
		_, err = layer.ListObjects(ctx, "", "", "", "/", 0)
		assert.Equal(t, minio.BucketNameInvalid{}, err)

		// Check the error when listing objects in a non-existing bucket
		_, err = layer.ListObjects(ctx, TestBucket, "", "", "", 0)
		assert.Equal(t, minio.BucketNotFound{Bucket: TestBucket}, err)

		// Create the bucket and files using the Metainfo API
		_, err = metainfo.CreateBucket(ctx, TestBucket, &storj.Bucket{PathCipher: storj.Unencrypted})
		assert.NoError(t, err)

		filePaths := []string{
			"a", "aa", "b", "bb", "c",
			"a/xa", "a/xaa", "a/xb", "a/xbb", "a/xc",
			"b/ya", "b/yaa", "b/yb", "b/ybb", "b/yc",
		}

		files := make(map[string]storj.Object, len(filePaths))
		createInfo := storj.CreateObject{
			ContentType: "text/plain",
			Metadata:    map[string]string{"key1": "value1", "key2": "value2"},
		}

		for _, filePath := range filePaths {
			file, err := createFile(ctx, metainfo, streams, TestBucket, filePath, &createInfo, []byte("test"))
			files[filePath] = file
			assert.NoError(t, err)
		}

		for i, tt := range []struct {
			prefix    string
			marker    string
			delimiter string
			maxKeys   int
			more      bool
			prefixes  []string
			objects   []string
		}{
			{
				delimiter: "/",
				prefixes:  []string{"a/", "b/"},
				objects:   []string{"a", "aa", "b", "bb", "c"},
			}, {
				marker:    "`",
				delimiter: "/",
				prefixes:  []string{"a/", "b/"},
				objects:   []string{"a", "aa", "b", "bb", "c"},
			}, {
				marker:    "b",
				delimiter: "/",
				prefixes:  []string{"b/"},
				objects:   []string{"bb", "c"},
			}, {
				marker:    "c",
				delimiter: "/",
			}, {
				marker:    "ca",
				delimiter: "/",
			}, {
				delimiter: "/",
				maxKeys:   1,
				more:      true,
				objects:   []string{"a"},
			}, {
				marker:    "`",
				delimiter: "/",
				maxKeys:   1,
				more:      true,
				objects:   []string{"a"},
			}, {
				marker:    "aa",
				delimiter: "/",
				maxKeys:   1,
				more:      true,
				objects:   []string{"b"},
			}, {
				marker:    "c",
				delimiter: "/",
				maxKeys:   1,
			}, {
				marker:    "ca",
				delimiter: "/",
				maxKeys:   1,
			}, {
				delimiter: "/",
				maxKeys:   2,
				more:      true,
				prefixes:  []string{"a/"},
				objects:   []string{"a"},
			}, {
				marker:    "`",
				delimiter: "/",
				maxKeys:   2,
				more:      true,
				prefixes:  []string{"a/"},
				objects:   []string{"a"},
			}, {
				marker:    "aa",
				delimiter: "/",
				maxKeys:   2,
				more:      true,
				prefixes:  []string{"b/"},
				objects:   []string{"b"},
			}, {
				marker:    "bb",
				delimiter: "/",
				maxKeys:   2,
				objects:   []string{"c"},
			}, {
				marker:    "c",
				delimiter: "/",
				maxKeys:   2,
			}, {
				marker:    "ca",
				delimiter: "/",
				maxKeys:   2,
			}, {
				objects: []string{"a", "a/xa", "a/xaa", "a/xb", "a/xbb", "a/xc", "aa", "b", "b/ya", "b/yaa", "b/yb", "b/ybb", "b/yc", "bb", "c"},
			}, {
				prefix:    "a",
				delimiter: "/",
				objects:   []string{"xa", "xaa", "xb", "xbb", "xc"},
			}, {
				prefix:    "a/",
				delimiter: "/",
				objects:   []string{"xa", "xaa", "xb", "xbb", "xc"},
			}, {
				prefix:    "a/",
				marker:    "xb",
				delimiter: "/",
				objects:   []string{"xbb", "xc"},
			}, {
				marker:  "a/xbb",
				maxKeys: 5,
				more:    true,
				objects: []string{"a/xc", "aa", "b", "b/ya", "b/yaa"},
			}, {
				prefix:    "a/",
				marker:    "xaa",
				delimiter: "/",
				maxKeys:   2,
				more:      true,
				objects:   []string{"xb", "xbb"},
			},
		} {
			errTag := fmt.Sprintf("%d. %+v", i, tt)

			// Check that the expected objects can be listed using the Minio API
			prefixes, objects, isTruncated, err := listObjects(ctx, layer, TestBucket, tt.prefix, tt.marker, tt.delimiter, tt.maxKeys)
			if assert.NoError(t, err, errTag) {
				assert.Equal(t, tt.more, isTruncated, errTag)
				assert.Equal(t, tt.prefixes, prefixes, errTag)
				assert.Equal(t, len(tt.objects), len(objects), errTag)
				for i, objectInfo := range objects {
					path := objectInfo.Name
					if tt.prefix != "" {
						path = storj.JoinPaths(strings.TrimSuffix(tt.prefix, "/"), path)
					}
					obj := files[path]

					assert.Equal(t, tt.objects[i], objectInfo.Name, errTag)
					assert.Equal(t, TestBucket, objectInfo.Bucket, errTag)
					assert.False(t, objectInfo.IsDir, errTag)
					assert.Equal(t, obj.Modified, objectInfo.ModTime, errTag)
					assert.Equal(t, obj.Size, objectInfo.Size, errTag)
					assert.Equal(t, hex.EncodeToString(obj.Checksum), objectInfo.ETag, errTag)
					assert.Equal(t, obj.ContentType, objectInfo.ContentType, errTag)
					assert.Equal(t, obj.Metadata, objectInfo.UserDefined, errTag)
				}
			}
		}
	})
}

func runTest(t *testing.T, test func(context.Context, minio.ObjectLayer, storj.Metainfo, streams.Store)) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 4, 1)
	if !assert.NoError(t, err) {
		return
	}

	defer ctx.Check(planet.Shutdown)

	planet.Start(ctx)

	layer, metainfo, streams, err := initEnv(planet)
	if !assert.NoError(t, err) {
		return
	}

	test(ctx, layer, metainfo, streams)
}

func initEnv(planet *testplanet.Planet) (minio.ObjectLayer, storj.Metainfo, streams.Store, error) {
	// TODO(kaloyan): We should have a better way for configuring the Satellite's API Key
	err := flag.Set("pointer-db.auth.api-key", TestAPIKey)
	if err != nil {
		return nil, nil, nil, err
	}

	oc, err := planet.Uplinks[0].DialOverlay(planet.Satellites[0])
	if err != nil {
		return nil, nil, nil, err
	}

	pdb, err := planet.Uplinks[0].DialPointerDB(planet.Satellites[0], TestAPIKey)
	if err != nil {
		return nil, nil, nil, err
	}

	ec := ecclient.NewClient(planet.Uplinks[0].Identity, 0)
	fc, err := infectious.NewFEC(2, 4)
	if err != nil {
		return nil, nil, nil, err
	}

	rs, err := eestream.NewRedundancyStrategy(eestream.NewRSScheme(fc, int(1*memory.KB)), 3, 4)
	if err != nil {
		return nil, nil, nil, err
	}

	segments := segments.NewSegmentStore(oc, ec, pdb, rs, int(8*memory.KB))

	key := new(storj.Key)
	copy(key[:], TestEncKey)

	streams, err := streams.NewStreamStore(segments, int64(64*memory.MB), key, int(1*memory.KB), storj.AESGCM)
	if err != nil {
		return nil, nil, nil, err
	}

	buckets := buckets.NewStore(streams)

	metainfo := kvmetainfo.New(buckets, streams, segments, pdb, key)

	gateway := NewStorjGateway(
		metainfo,
		streams,
		storj.AESGCM,
		storj.EncryptionScheme{
			Cipher:    storj.AESGCM,
			BlockSize: 1 * memory.KB.Int32(),
		},
		storj.RedundancyScheme{
			Algorithm:      storj.ReedSolomon,
			RequiredShares: int16(rs.RequiredCount()),
			RepairShares:   int16(rs.RepairThreshold()),
			OptimalShares:  int16(rs.OptimalThreshold()),
			TotalShares:    int16(rs.TotalCount()),
			ShareSize:      int32(rs.ErasureShareSize()),
		},
	)

	layer, err := gateway.NewGatewayLayer(auth.Credentials{})

	return layer, metainfo, streams, err
}

func createFile(ctx context.Context, metainfo storj.Metainfo, streams streams.Store, bucket string, path storj.Path, createInfo *storj.CreateObject, data []byte) (storj.Object, error) {
	mutableObject, err := metainfo.CreateObject(ctx, bucket, path, createInfo)
	if err != nil {
		return storj.Object{}, err
	}

	err = upload(ctx, streams, mutableObject, bytes.NewReader(data))
	if err != nil {
		return storj.Object{}, err
	}

	err = mutableObject.Commit(ctx)
	if err != nil {
		return storj.Object{}, err
	}

	return mutableObject.Info(), nil
}
