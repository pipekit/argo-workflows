// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
package dag

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper types for store testing ---

// storeTestValue implements Value for testing.
type storeTestValue struct {
	data string
}

func (v storeTestValue) Hash() Hash {
	return Hash(v.data)
}

func newStoreTestValue(data string) storeTestValue {
	return storeTestValue{data: data}
}

// --- Tests for MapStore ---

func TestMapStore_NewMapStore(t *testing.T) {
	t.Run("creates empty store with initial info", func(t *testing.T) {
		initialInfo := "initial"
		store := NewMapStore[string, Key, storeTestValue](initialInfo)

		assert.NotNil(t, store)
		assert.Equal(t, initialInfo, store.GetInfo())
		assert.Empty(t, store.ListKeys())
	})

	t.Run("creates store with nil initial info", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		assert.NotNil(t, store)
		assert.Nil(t, store.GetInfo())
	})
}

func TestMapStore_GetSetValue(t *testing.T) {
	t.Run("set and get value", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		value := newStoreTestValue("test-data")
		store.SetValue("key1", value)

		retrieved, ok := store.GetValue("key1")
		assert.True(t, ok)
		assert.Equal(t, value.data, retrieved.data)
	})

	t.Run("get nonexistent value returns false", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		_, ok := store.GetValue("nonexistent")
		assert.False(t, ok)
	})

	t.Run("overwrite existing value", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetValue("key1", newStoreTestValue("original"))
		store.SetValue("key1", newStoreTestValue("updated"))

		retrieved, ok := store.GetValue("key1")
		assert.True(t, ok)
		assert.Equal(t, "updated", retrieved.data)
	})
}

func TestMapStore_GetSetInfo(t *testing.T) {
	t.Run("get and set info", func(t *testing.T) {
		store := NewMapStore[string, Key, storeTestValue]("initial")

		assert.Equal(t, "initial", store.GetInfo())

		store.SetInfo("updated")
		assert.Equal(t, "updated", store.GetInfo())
	})

	t.Run("set complex info type", func(t *testing.T) {
		type complexInfo struct {
			name    string
			version int
		}
		store := NewMapStore[*complexInfo, Key, storeTestValue](&complexInfo{name: "test", version: 1})

		store.SetInfo(&complexInfo{name: "updated", version: 2})

		info := store.GetInfo()
		assert.Equal(t, "updated", info.name)
		assert.Equal(t, 2, info.version)
	})
}

func TestMapStore_GetSetState(t *testing.T) {
	t.Run("get state returns pending for unknown key", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		state := store.GetState("nonexistent")
		assert.Equal(t, TaskStatePending, state)
	})

	t.Run("set and get state", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetState("key1", TaskStateRunning)
		assert.Equal(t, TaskStateRunning, store.GetState("key1"))

		store.SetState("key1", TaskStateSucceeded)
		assert.Equal(t, TaskStateSucceeded, store.GetState("key1"))
	})

	t.Run("different states for different keys", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetState("key1", TaskStateRunning)
		store.SetState("key2", TaskStateSucceeded)
		store.SetState("key3", TaskStateFailed)

		assert.Equal(t, TaskStateRunning, store.GetState("key1"))
		assert.Equal(t, TaskStateSucceeded, store.GetState("key2"))
		assert.Equal(t, TaskStateFailed, store.GetState("key3"))
	})
}

func TestMapStore_ListKeys(t *testing.T) {
	t.Run("lists keys from values and states", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetValue("key1", newStoreTestValue("data1"))
		store.SetState("key2", TaskStateRunning)
		store.SetValue("key3", newStoreTestValue("data3"))
		store.SetState("key3", TaskStateSucceeded)

		keys := store.ListKeys()

		// Should have 3 unique keys
		assert.Len(t, keys, 3)
		assert.Contains(t, keys, Key("key1"))
		assert.Contains(t, keys, Key("key2"))
		assert.Contains(t, keys, Key("key3"))
	})

	t.Run("empty store returns empty list", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		keys := store.ListKeys()
		assert.Empty(t, keys)
	})
}

func TestMapStore_HasValue(t *testing.T) {
	t.Run("returns true when value exists", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetValue("key1", newStoreTestValue("data"))

		assert.True(t, store.HasValue("key1"))
	})

	t.Run("returns false when value does not exist", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		assert.False(t, store.HasValue("nonexistent"))
	})

	t.Run("returns false after delete", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetValue("key1", newStoreTestValue("data"))
		assert.True(t, store.HasValue("key1"))

		store.DeleteValue("key1")
		assert.False(t, store.HasValue("key1"))
	})
}

func TestMapStore_DeleteValue(t *testing.T) {
	t.Run("deletes existing value", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetValue("key1", newStoreTestValue("data"))
		store.DeleteValue("key1")

		_, ok := store.GetValue("key1")
		assert.False(t, ok)
	})

	t.Run("delete nonexistent key is no-op", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		// Should not panic
		store.DeleteValue("nonexistent")
	})
}

func TestMapStore_DeleteState(t *testing.T) {
	t.Run("deletes existing state", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		store.SetState("key1", TaskStateSucceeded)
		store.DeleteState("key1")

		// After delete, should return default (Pending)
		assert.Equal(t, TaskStatePending, store.GetState("key1"))
	})

	t.Run("delete nonexistent state is no-op", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)

		// Should not panic
		store.DeleteState("nonexistent")
	})
}

func TestMapStore_Clear(t *testing.T) {
	t.Run("clears all values and states but keeps info", func(t *testing.T) {
		store := NewMapStore[string, Key, storeTestValue]("my-info")

		store.SetValue("key1", newStoreTestValue("data1"))
		store.SetValue("key2", newStoreTestValue("data2"))
		store.SetState("key1", TaskStateSucceeded)
		store.SetState("key3", TaskStateFailed)

		store.Clear()

		assert.Empty(t, store.ListKeys())
		assert.False(t, store.HasValue("key1"))
		assert.False(t, store.HasValue("key2"))
		assert.Equal(t, TaskStatePending, store.GetState("key1"))
		assert.Equal(t, TaskStatePending, store.GetState("key3"))

		// Info should be preserved
		assert.Equal(t, "my-info", store.GetInfo())
	})
}

func TestMapStore_ThreadSafety(t *testing.T) {
	t.Run("concurrent reads and writes", func(t *testing.T) {
		store := NewMapStore[int, Key, storeTestValue](0)
		const goroutines = 100
		const iterations = 100

		var wg sync.WaitGroup
		wg.Add(goroutines * 2)

		// Writers
		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				key := Key("key")
				for j := 0; j < iterations; j++ {
					store.SetValue(key, newStoreTestValue("data"))
					store.SetState(key, TaskState(j%7))
					store.SetInfo(id*iterations + j)
				}
			}(i)
		}

		// Readers
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				key := Key("key")
				for j := 0; j < iterations; j++ {
					store.GetValue(key)
					store.GetState(key)
					store.GetInfo()
					store.ListKeys()
					store.HasValue(key)
				}
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent operations on different keys", func(t *testing.T) {
		store := NewMapStore[any, Key, storeTestValue](nil)
		const goroutines = 50

		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				key := Key(string(rune('A' + id)))
				store.SetValue(key, newStoreTestValue(key))
				store.SetState(key, TaskStateSucceeded)
				v, ok := store.GetValue(key)
				if ok && v.data != key {
					t.Errorf("unexpected value for key %s: got %s", key, v.data)
				}
			}(i)
		}

		wg.Wait()

		// All keys should be present
		keys := store.ListKeys()
		assert.Len(t, keys, goroutines)
	})
}

// --- Tests for KeyInfoStore ---

func TestKeyInfoStore_NewKeyInfoStore(t *testing.T) {
	t.Run("creates empty store", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		assert.NotNil(t, store)
		assert.Empty(t, store.ListKeys())
	})
}

func TestKeyInfoStore_GetSetKeyInfo(t *testing.T) {
	t.Run("set and get key info", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		store.SetKeyInfo("key1", "info1")
		store.SetKeyInfo("key2", "info2")

		info1, ok1 := store.GetKeyInfo("key1")
		assert.True(t, ok1)
		assert.Equal(t, "info1", info1)

		info2, ok2 := store.GetKeyInfo("key2")
		assert.True(t, ok2)
		assert.Equal(t, "info2", info2)
	})

	t.Run("get nonexistent key info", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		info, ok := store.GetKeyInfo("nonexistent")
		assert.False(t, ok)
		assert.Equal(t, "", info)
	})

	t.Run("overwrite existing key info", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		store.SetKeyInfo("key1", "original")
		store.SetKeyInfo("key1", "updated")

		info, ok := store.GetKeyInfo("key1")
		assert.True(t, ok)
		assert.Equal(t, "updated", info)
	})
}

func TestKeyInfoStore_DeleteKeyInfo(t *testing.T) {
	t.Run("deletes key info", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		store.SetKeyInfo("key1", "info1")
		store.DeleteKeyInfo("key1")

		_, ok := store.GetKeyInfo("key1")
		assert.False(t, ok)
	})

	t.Run("delete nonexistent key info is no-op", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		// Should not panic
		store.DeleteKeyInfo("nonexistent")
	})
}

func TestKeyInfoStore_InheritsMapStore(t *testing.T) {
	t.Run("can use inherited MapStore methods", func(t *testing.T) {
		store := NewKeyInfoStore[string, Key, storeTestValue]()

		// Set value using inherited method
		store.SetValue("key1", newStoreTestValue("data"))

		// Get value using inherited method
		value, ok := store.GetValue("key1")
		assert.True(t, ok)
		assert.Equal(t, "data", value.data)

		// Set state using inherited method
		store.SetState("key1", TaskStateSucceeded)
		assert.Equal(t, TaskStateSucceeded, store.GetState("key1"))

		// List keys
		keys := store.ListKeys()
		assert.Contains(t, keys, Key("key1"))
	})
}

func TestKeyInfoStore_ComplexInfoType(t *testing.T) {
	t.Run("works with complex info types", func(t *testing.T) {
		type traceInfo struct {
			dependencies []string
			hash         string
		}

		store := NewKeyInfoStore[*traceInfo, Key, storeTestValue]()

		store.SetKeyInfo("taskA", &traceInfo{
			dependencies: []string{"dep1", "dep2"},
			hash:         "abc123",
		})

		info, ok := store.GetKeyInfo("taskA")
		require.True(t, ok)
		assert.Equal(t, []string{"dep1", "dep2"}, info.dependencies)
		assert.Equal(t, "abc123", info.hash)
	})
}

// --- Tests for Store interface ---

func TestStoreInterfaceCompliance(t *testing.T) {
	t.Run("MapStore implements Store interface", func(t *testing.T) {
		var store Store[any, Key, storeTestValue] = NewMapStore[any, Key, storeTestValue](nil)
		assert.NotNil(t, store)

		// Test all interface methods
		store.SetValue("key1", newStoreTestValue("data"))
		val, ok := store.GetValue("key1")
		assert.True(t, ok)
		assert.Equal(t, "data", val.data)

		store.SetInfo("info")
		assert.Equal(t, "info", store.GetInfo())

		store.SetState("key1", TaskStateSucceeded)
		assert.Equal(t, TaskStateSucceeded, store.GetState("key1"))

		keys := store.ListKeys()
		assert.Contains(t, keys, Key("key1"))
	})

	t.Run("KeyInfoStore implements Store interface", func(t *testing.T) {
		var store Store[map[Key]string, Key, storeTestValue] = NewKeyInfoStore[string, Key, storeTestValue]()
		assert.NotNil(t, store)

		// Test all interface methods
		store.SetValue("key1", newStoreTestValue("data"))
		val, ok := store.GetValue("key1")
		assert.True(t, ok)
		assert.Equal(t, "data", val.data)

		store.SetState("key1", TaskStateSucceeded)
		assert.Equal(t, TaskStateSucceeded, store.GetState("key1"))

		keys := store.ListKeys()
		assert.Contains(t, keys, Key("key1"))
	})
}
