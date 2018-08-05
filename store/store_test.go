package store

import (
	"testing"

	"github.com/loomnetwork/go-loom/plugin"
	"github.com/loomnetwork/go-loom/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tmlibs/db"
)

var (
	key1 = []byte("1234")
	key2 = []byte("af876")
	key3 = []byte("9876")
	val1 = []byte("value1")
	val2 = []byte("value2")
	val3 = []byte("value3")
)

func TestCacheTxCommit(t *testing.T) {
	tests := []struct {
		tx tempTx
	}{
		{
			tempTx{Action: txSet, Key: key1, Value: val1},
		},
		{
			tempTx{Action: txSet, Key: key2, Value: val2},
		},
		{
			tempTx{Action: txSet, Key: key3, Value: val3},
		},
		{
			tempTx{Action: txDelete, Key: key1},
		},
		{
			tempTx{Action: txDelete, Key: key2},
		},
		{
			tempTx{Action: txSet, Key: key2, Value: val2},
		},
	}
	s := NewMemStore()
	cs := newCacheTx(s)

	for _, test := range tests {
		switch test.tx.Action {
		case txSet:
			cs.Set(test.tx.Key, test.tx.Value)
		case txDelete:
			cs.Delete(test.tx.Key)
		}
	}

	// ordering
	for i, test := range tests {
		tx := cs.tmpTxs[i]
		assert.Equal(t, tx.Action, test.tx.Action)
		assert.Equal(t, tx.Key, test.tx.Key)
		assert.Equal(t, tx.Value, test.tx.Value)
	}

	// cache functionality
	v1 := cs.Get(key1)
	assert.Nil(t, v1)
	v2 := cs.Get(key2)
	assert.Equal(t, val2, v2)
	v3 := cs.Get(key3)
	assert.Equal(t, val3, v3)

	// underlying store should not be modified
	v1 = s.Get(key1)
	assert.Nil(t, v1)
	v2 = s.Get(key2)
	assert.Nil(t, v2)
	v3 = s.Get(key3)
	assert.Nil(t, v3)

	// commit
	cs.Commit()
	v1 = cs.Get(key1)
	assert.Nil(t, v1)
	v2 = cs.Get(key2)
	assert.Equal(t, val2, v2)
	v3 = cs.Get(key3)
	assert.Equal(t, val3, v3)

	// underlying store should be modified
	v1 = s.Get(key1)
	assert.Nil(t, v1)
	v2 = s.Get(key2)
	assert.Equal(t, val2, v2)
	v3 = s.Get(key3)
	assert.Equal(t, val3, v3)
}

func TestCacheTxRollback(t *testing.T) {
	tests := []struct {
		tx tempTx
	}{
		{
			tempTx{Action: txSet, Key: key1, Value: val1},
		},
		{
			tempTx{Action: txSet, Key: key2, Value: val2},
		},
		{
			tempTx{Action: txSet, Key: key3, Value: val3},
		},
		{
			tempTx{Action: txDelete, Key: key1},
		},
		{
			tempTx{Action: txDelete, Key: key2},
		},
		{
			tempTx{Action: txSet, Key: key2, Value: val2},
		},
	}
	s := NewMemStore()
	cs := newCacheTx(s)

	for _, test := range tests {
		switch test.tx.Action {
		case txSet:
			cs.Set(test.tx.Key, test.tx.Value)
		case txDelete:
			cs.Delete(test.tx.Key)
		}
	}

	// ordering
	for i, test := range tests {
		tx := cs.tmpTxs[i]
		assert.Equal(t, tx.Action, test.tx.Action)
		assert.Equal(t, tx.Key, test.tx.Key)
		assert.Equal(t, tx.Value, test.tx.Value)
	}

	// cache functionality
	v1 := cs.Get(key1)
	assert.Nil(t, v1)
	v2 := cs.Get(key2)
	assert.Equal(t, val2, v2)
	v3 := cs.Get(key3)
	assert.Equal(t, val3, v3)

	// underlying store should not be modified
	v1 = s.Get(key1)
	assert.Nil(t, v1)
	v2 = s.Get(key2)
	assert.Nil(t, v2)
	v3 = s.Get(key3)
	assert.Nil(t, v3)

	// rollback
	cs.Rollback()
	v1 = cs.Get(key1)
	assert.Nil(t, v1)
	v2 = cs.Get(key2)
	assert.Nil(t, v2)
	v3 = cs.Get(key3)
	assert.Nil(t, v3)

	// underlying store should not be modified
	v1 = s.Get(key1)
	assert.Nil(t, v1)
	v2 = s.Get(key2)
	assert.Nil(t, v2)
	v3 = s.Get(key3)
	assert.Nil(t, v3)
}

type storeTestFactory func(t *testing.T) (KVStore, string)

func TestStoreRange(t *testing.T) {
	factories := []storeTestFactory{
		func(t *testing.T) (KVStore, string) {
			db := dbm.NewMemDB()
			s, err := NewIAVLStore(db)
			require.NoError(t, err)
			return s, "IAVLStore"
		},
		func(t *testing.T) (KVStore, string) {
			return NewMemStore(), "MemStore"
		},
	}

	for _, f := range factories {
		s, storeName := f(t)

		prefix1 := []byte("doremi")
		prefix2 := append([]byte("stop"), byte(255))
		prefix3 := append([]byte("stop"), byte(0))

		entries := []*plugin.RangeEntry{
			&plugin.RangeEntry{Key: []byte("abc"), Value: []byte("1")},
			&plugin.RangeEntry{Key: []byte("abc123"), Value: []byte("2")},

			&plugin.RangeEntry{Key: util.PrefixKey(prefix1, []byte("1")), Value: []byte("3")},
			&plugin.RangeEntry{Key: util.PrefixKey(prefix1, []byte("2")), Value: []byte("4")},
			&plugin.RangeEntry{Key: util.PrefixKey(prefix1, []byte("3")), Value: []byte("5")},

			&plugin.RangeEntry{Key: util.PrefixKey(prefix2, []byte("3")), Value: []byte("6")},
			&plugin.RangeEntry{Key: util.PrefixKey(prefix2, []byte("2")), Value: []byte("7")},
			&plugin.RangeEntry{Key: util.PrefixKey(prefix2, []byte("1")), Value: []byte("8")},
			&plugin.RangeEntry{Key: util.PrefixKey(prefix2, []byte("4")), Value: []byte("9")},

			&plugin.RangeEntry{Key: util.PrefixKey(prefix3, []byte{byte(0)}), Value: []byte("10")},
			&plugin.RangeEntry{Key: util.PrefixKey(prefix3, []byte{byte(255)}), Value: []byte("11")},
		}
		for _, e := range entries {
			s.Set(e.Key, e.Value)
		}

		expected := []*plugin.RangeEntry{
			entries[0],
			entries[1],
		}
		actual := s.Range([]byte("abc"))

		require.Len(t, actual, 2)
		if storeName != "MemStore" { // TODO: MemStore iteration order should be stable, no random
			for i := range expected {
				require.EqualValues(t, expected[i], actual[i], storeName)
			}
		}

		require.Len(t, s.Range([]byte("abc123")), 1)
		require.EqualValues(t, entries[1], s.Range([]byte("abc123"))[0], storeName)

		expected = []*plugin.RangeEntry{
			entries[2],
			entries[3],
			entries[4],
		}
		actual = s.Range(prefix1)
		require.Len(t, actual, len(expected), storeName)
		if storeName != "MemStore" {
			for i := range expected {
				require.EqualValues(t, expected[i], actual[i], storeName)
			}
		}

		expected = []*plugin.RangeEntry{
			entries[7],
			entries[6],
			entries[5],
			entries[8],
		}
		actual = s.Range(prefix2)
		require.Len(t, actual, len(expected), storeName)
		// TODO: MemStore keys should be iterated in ascending order
		if storeName != "MemStore" {
			for i := range expected {
				require.EqualValues(t, expected[i], actual[i], storeName)
			}
		}

		expected = []*plugin.RangeEntry{
			entries[9],
			entries[10],
		}
		actual = s.Range(prefix3)
		require.Len(t, actual, len(expected), storeName)
		if storeName != "MemStore" {
			for i := range expected {
				require.EqualValues(t, expected[i], actual[i], storeName)
			}
		}
	}
}
