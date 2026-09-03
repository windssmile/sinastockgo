package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"sinago/sina"
	"sinago/ui"
)

// 首次启动的默认自选，覆盖各个品种大类。
var defaults = []ui.Item{
	{Code: "sh000001", Name: "上证指数"},
	{Code: "sz300059", Name: "东方财富"},
	{Code: "rt_hk00700", Name: "腾讯控股"},
	{Code: "gb_$ixic", Name: "纳斯达克"},
	{Code: "hf_CL", Name: "纽约原油"},
	{Code: "nf_IF0", Name: "沪深300期货"},
	{Code: "fx_susdcny", Name: "美元人民币"},
	{Code: "btc_btcbtcusd", Name: "比特币美元"},
}

func watchlistPath() string { return filepath.Join(configDir(), "watchlist.json") }

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "sinago")
}

// logKeep 是日志保留期，启动时清掉更早的。
const logKeep = 7 * 24 * time.Hour

// setupLog 把结构化日志按天写到 sinago-YYYY-MM-DD.log，一行一个 JSON 事件，
// 方便事后批量统计（jq 一把梭：断线频率、每条连接活了多久、每分钟多少行情）。
// 终端被 TUI 占着，日志绝不能走 stdout/stderr。
// ponytail: 跨零点不切文件，当天的日志全写进启动那天——重启即换新文件，够用了。
func setupLog() func() {
	discard := func() func() {
		slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
		return func() {}
	}
	if os.MkdirAll(configDir(), 0o755) != nil {
		return discard()
	}
	name := "sinago-" + time.Now().Format(time.DateOnly) + ".log"
	f, err := os.OpenFile(filepath.Join(configDir(), name),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return discard()
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(f, nil)))
	if n := pruneLogs(configDir(), logKeep); n > 0 {
		slog.Info("prune_logs", "removed", n)
	}
	return func() { f.Close() }
}

// pruneLogs 删掉超过 keep 的日志，返回删除条数。按文件名里的日期判断而不是
// mtime——跑了一整天的进程会一直在写当天那个文件，mtime 是最后一次写入时间，
// 对当天文件没差，但用日期更直白，也不会被 cp/rsync 改掉的 mtime 骗到。
// glob 只匹配自己写的 sinago-*.log，绝不碰同目录的 watchlist.json。
func pruneLogs(dir string, keep time.Duration) int {
	paths, err := filepath.Glob(filepath.Join(dir, "sinago-*.log"))
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-keep)
	n := 0
	for _, p := range paths {
		base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "sinago-"), ".log")
		day, err := time.ParseInLocation(time.DateOnly, base, time.Local)
		if err != nil {
			continue // 名字对不上格式的不动，不是我们写的
		}
		// 比的是那天的结束时刻：保留期 7 天，今天往前数第 7 天当天仍算数。
		if day.AddDate(0, 0, 1).Before(cutoff) {
			if os.Remove(p) == nil {
				n++
			}
		}
	}
	return n
}

func load(path string) []ui.Item {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	var items []ui.Item
	if json.Unmarshal(b, &items) != nil || len(items) == 0 {
		return defaults
	}
	return items
}

func save(path string, items []ui.Item) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	// 先写临时文件再改名，避免写到一半崩溃留下半截 JSON 导致自选丢失。
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, path)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defer setupLog()()
	slog.Info("start")

	path := watchlistPath()
	feed := sina.NewFeed()
	go feed.Run(ctx)

	app := ui.New(feed, load(path), func(items []ui.Item) { save(path, items) })
	if err := app.Run(ctx); err != nil {
		slog.Error("exit", "err", err)
		log.Fatal(err)
	}
	slog.Info("stop")
}
