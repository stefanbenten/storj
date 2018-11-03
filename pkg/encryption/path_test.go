// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package encryption

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"storj.io/storj/pkg/storj"
)

func TestEncryption(t *testing.T) {
	for i, path := range []storj.Path{
		"",
		"/",
		"//",
		"file.txt",
		"file.txt/",
		"fold1/file.txt",
		"fold1/fold2/file.txt",
		"/fold1/fold2/fold3/file.txt",
	} {
		errTag := fmt.Sprintf("Test case #%d", i)

		key := new(storj.Key)
		copy(key[:], randData(storj.KeySize))

		encrypted, err := EncryptPath(path, key)
		if !assert.NoError(t, err, errTag) {
			continue
		}

		decrypted, err := DecryptPath(encrypted, key)
		if !assert.NoError(t, err, errTag) {
			continue
		}

		assert.Equal(t, path, decrypted, errTag)
	}
}

func TestDeriveKey(t *testing.T) {
	for i, tt := range []struct {
		path      storj.Path
		depth     int
		errString string
	}{
		{"fold1/fold2/fold3/file.txt", -1, "encryption error: negative depth"},
		{"fold1/fold2/fold3/file.txt", 0, ""},
		{"fold1/fold2/fold3/file.txt", 1, ""},
		{"fold1/fold2/fold3/file.txt", 2, ""},
		{"fold1/fold2/fold3/file.txt", 3, ""},
		{"fold1/fold2/fold3/file.txt", 4, ""},
		{"fold1/fold2/fold3/file.txt", 5, "encryption error: depth greater than path length"},
	} {
		errTag := fmt.Sprintf("Test case #%d", i)

		key := new(storj.Key)
		copy(key[:], randData(storj.KeySize))

		encrypted, err := EncryptPath(tt.path, key)
		if !assert.NoError(t, err, errTag) {
			continue
		}

		derivedKey, err := DerivePathKey(tt.path, key, tt.depth)
		if tt.errString != "" {
			assert.EqualError(t, err, tt.errString, errTag)
			continue
		}
		if !assert.NoError(t, err, errTag) {
			continue
		}

		shared := storj.JoinPaths(storj.SplitPath(encrypted)[tt.depth:]...)
		decrypted, err := DecryptPath(shared, derivedKey)
		if !assert.NoError(t, err, errTag) {
			continue
		}

		expected := storj.JoinPaths(storj.SplitPath(tt.path)[tt.depth:]...)
		assert.Equal(t, expected, decrypted, errTag)
	}
}
