package sina

import (
	"context"
	"testing"
	"time"
)

// TestFeedDeliversSnapshot 验证 Run 起来后能拿到快照，且改自选会触发重连并重发。
// 这条覆盖的是「WSS 只推变化、休市时一条不给」这个坑：若去掉快照步骤，
// 非交易时段跑这个测试会超时。
func TestFeedDeliversSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过实网测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	f := NewFeed()
	go f.Run(ctx)

	f.Set([]string{"sh000001"})
	first := waitFor(t, ctx, f, "sh000001")
	if first.Name != "上证指数" {
		t.Fatalf("首批快照 name=%q，期望 上证指数", first.Name)
	}

	// 改自选后必须自动重连并把新代码的快照送上来。
	f.Set([]string{"sh000001", "gb_aapl"})
	if q := waitFor(t, ctx, f, "gb_aapl"); q.Last == 0 {
		t.Fatalf("重连后未收到 gb_aapl 行情")
	}
}

func waitFor(t *testing.T, ctx context.Context, f *Feed, code string) Quote {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case qs := <-f.Out:
			for _, q := range qs {
				if q.Code == code {
					return q
				}
			}
		case <-deadline:
			t.Fatalf("等待 %s 超时", code)
		case <-ctx.Done():
			t.Fatalf("上下文取消")
		}
	}
}

func TestBackoffAfter(t *testing.T) {
	// 短命连接：翻倍并封顶 30s
	for _, c := range []struct{ prev, want time.Duration }{
		{500 * time.Millisecond, time.Second},
		{16 * time.Second, 30 * time.Second},
		{30 * time.Second, 30 * time.Second},
	} {
		if got := backoffAfter(2*time.Second, c.prev); got != c.want {
			t.Errorf("backoffAfter(2s, %v) = %v, want %v", c.prev, got, c.want)
		}
	}
	// 连上撑过一分钟的，重头来过而不是卡在 30s
	if got := backoffAfter(5*time.Minute, 30*time.Second); got != time.Second {
		t.Errorf("长连接断开后应重置退避，got %v", got)
	}
}

func TestIncrementalSnapshotAndForceFull(t *testing.T) {
	f := NewFeed()
	f.Set([]string{"sh000001", "sz000001"})
	fresh := map[string]bool{"sh000001": true, "sz000001": true}

	// 加一只：只拉新增的那只
	f.Set([]string{"sh000001", "sz000001", "gb_aapl"})
	if got := missing(f.current(), fresh); len(got) != 1 || got[0] != "gb_aapl" {
		t.Fatalf("增量快照应只拉 gb_aapl，got %v", got)
	}
	if f.takeFull() {
		t.Fatal("列表变了不该标记全量")
	}

	// r 刷新：列表没变，必须强制全量，否则什么都不拉
	f.Set(f.current())
	if !f.takeFull() {
		t.Fatal("列表没变应视为手动刷新，强制全量")
	}
	if f.takeFull() {
		t.Fatal("takeFull 应清掉标记")
	}

	// 快照失败后要能重来
	f.forceFull()
	if !f.takeFull() {
		t.Fatal("forceFull 未生效")
	}
}
