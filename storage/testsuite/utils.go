// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package testsuite

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"storj.io/storj/storage"
)

func newItem(key, value string, isPrefix bool) storage.ListItem {
	return storage.ListItem{
		Key:      storage.Key(key),
		Value:    storage.Value(value),
		IsPrefix: isPrefix,
	}
}

func cleanupItems(store storage.KeyValueStore, items storage.Items) {
	for _, item := range items {
		_ = store.Delete(item.Key)
	}
}

type iterationTest struct {
	Name     string
	Options  storage.IterateOptions
	Expected storage.Items
}

func testIterations(t *testing.T, store storage.KeyValueStore, tests []iterationTest) {
	t.Helper()
	for _, test := range tests {
		items, err := iterateItems(store, test.Options, -1)
		if err != nil {
			t.Errorf("%s: %v", test.Name, err)
			continue
		}
		if diff := cmp.Diff(test.Expected, items, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("%s: (-want +got)\n%s", test.Name, diff)
		}
	}
}

func isEmptyKVStore(tb testing.TB, store storage.KeyValueStore) bool {
	tb.Helper()
	keys, err := store.List(storage.Key(""), 1)
	if err != nil {
		tb.Fatalf("Failed to check if KeyValueStore is empty: %v", err)
	}
	return len(keys) == 0
}

type collector struct {
	Items storage.Items
	Limit int
}

func (collect *collector) include(it storage.Iterator) error {
	var item storage.ListItem
	for (collect.Limit < 0 || len(collect.Items) < collect.Limit) && it.Next(&item) {
		collect.Items = append(collect.Items, storage.CloneItem(item))
	}
	return nil
}

func iterateItems(store storage.KeyValueStore, opts storage.IterateOptions, limit int) (storage.Items, error) {
	collect := &collector{Limit: limit}
	err := store.Iterate(opts, collect.include)
	if err != nil {
		return nil, err
	}
	return collect.Items, nil
}
