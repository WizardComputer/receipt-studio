package app

import (
	"testing"
)

func TestDataDirectoryLockExcludesSecondAgent(t *testing.T) {
	dir := t.TempDir()
	release, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lockDataDir(dir)
	if err == nil {
		again()
		release()
		t.Fatal("two processes may access the same data")
	}
	other, err := lockDataDir(t.TempDir())
	if err != nil {
		release()
		t.Fatal(err)
	}
	other()
	release()
	release, err = lockDataDir(dir)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	release()
}
