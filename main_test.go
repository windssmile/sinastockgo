package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneLogs(t *testing.T) {
	dir := t.TempDir()
	day := func(d int) string {
		return "sinago-" + time.Now().AddDate(0, 0, d).Format(time.DateOnly) + ".log"
	}
	keep := []string{day(0), day(-1), day(-6), "watchlist.json", "sinago.log", "sinago-坏名字.log"}
	drop := []string{day(-8), day(-30)}
	for _, n := range append(append([]string{}, keep...), drop...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if n := pruneLogs(dir, logKeep); n != len(drop) {
		t.Fatalf("删了 %d 个，期望 %d", n, len(drop))
	}
	for _, n := range keep {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s 不该被删: %v", n, err)
		}
	}
	for _, n := range drop {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s 应被删掉", n)
		}
	}
}

// 第 7 天当天要留住，别把边界算成 6 天。
func TestPruneKeepsExactlySevenDays(t *testing.T) {
	dir := t.TempDir()
	edge := "sinago-" + time.Now().AddDate(0, 0, -7).Format(time.DateOnly) + ".log"
	os.WriteFile(filepath.Join(dir, edge), []byte("x"), 0o644)
	if n := pruneLogs(dir, logKeep); n != 0 {
		t.Fatalf("第 7 天的日志被删了")
	}
}
