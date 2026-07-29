package settings

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreSaveAndLoad(t *testing.T) {
	store := NewAt(filepath.Join(t.TempDir(), "settings.json"))
	want := Config{
		IgnoreHiddenFolders: true,
		ExcludedFolders:     []string{"dist", "nested/cache"},
		Workers:             6,
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestStoreNormalizesExcludedFolders(t *testing.T) {
	store := NewAt(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Save(Config{ExcludedFolders: []string{" dist ", "dist", "nested\\cache", "../outside", ""}, Workers: -1}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{ExcludedFolders: []string{"dist", filepath.Join("nested", "cache")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}
