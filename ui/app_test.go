package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"sinago/sina"
)

// pad 是搜索结果列对齐的唯一依据。中文占两格，用 %-24s 之类按 rune 数补齐
// 会让每行歪掉不同的格数——排查起来只会觉得「看着有点乱」，很难定位。
func TestPadUsesDisplayWidth(t *testing.T) {
	cases := []string{
		"aapl",                        // 纯 ASCII
		"腾讯控股",                        // 纯中文
		"AAPL（苹果暗盘）",                  // 中英混排 + 全角括号
		"沪深300指数期货连续",                 // 超长，需截断
		"HSBC Holdings plc ADR hedge", // 超长 ASCII
		"",                            // 空串
	}
	for _, w := range []int{10, 24} {
		for _, s := range cases {
			got := pad(s, w)
			if n := tview.TaggedStringWidth(got); n != w {
				t.Errorf("pad(%q, %d) 显示宽度 = %d，期望 %d（结果 %q）", s, w, n, w, got)
			}
		}
	}
}

// 列宽定死是防止行情一跳整张表横向平移的唯一依据：9.99→10.00、
// 万→亿 都不能改变单元格显示宽度。
func TestNumericCellsAreFixedWidth(t *testing.T) {
	for _, c := range []struct {
		col  int
		vals []string
	}{
		{2, []string{price(9.99), price(10), price(1234.5), "-"}},
		{6, []string{compact(0), compact(1879e4), compact(9.7e11), compact(123)}},
	} {
		for _, v := range c.vals {
			if n := tview.TaggedStringWidth(padL(v, widths[c.col])); n != widths[c.col] {
				t.Errorf("列 %d 的 %q 宽 %d，期望 %d", c.col, v, n, widths[c.col])
			}
		}
	}
	if len(widths) != len(columns) {
		t.Fatalf("widths %d 项，columns %d 项", len(widths), len(columns))
	}
}

// 重绘走的是「排队更新里再触发一次绘制」这条路，用错 API（Draw 而非
// ForceDraw）不会报错也不会 panic——只是整个界面从此不再出数据。
// 跑一遍真的事件循环，确认行情能落到屏幕上。
func TestQuotesReachTheScreen(t *testing.T) {
	feed := sina.NewFeed()
	a := New(feed, []Item{{Code: "sz300059", Name: "东方财富"}}, func([]Item) {})
	sim := tcell.NewSimulationScreen("UTF-8")
	a.app.SetScreen(sim)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	defer a.app.Stop()

	feed.Out <- []sina.Quote{{Code: "sz300059", Name: "东方财富", Last: 19.42}}
	feed.Out <- []sina.Quote{{Code: "sz300059", Name: "东方财富", Last: 19.45}} // 触发高亮

	deadline := time.Now().Add(3 * time.Second)
	for {
		cells, w, _ := sim.GetContents()
		var b strings.Builder
		for i, c := range cells {
			if i%w == 0 {
				b.WriteByte('\n')
			}
			b.WriteString(string(c.Runes))
		}
		if strings.Contains(b.String(), "19.450") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("3 秒内屏幕上没出现最新价，界面卡死了。当前内容:%s", b.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// 三市指数必须常驻订阅（底栏成交额靠它们），但用户已经自选了的不能订两遍。
func TestBaseCodesAlwaysSubscribedOnce(t *testing.T) {
	a := &App{quo: map[string]sina.Quote{}, items: []Item{
		{Code: "sh000001"}, {Code: "sz300059"},
	}}
	got := a.codes()
	for _, b := range baseCodes {
		if n := strings.Count(strings.Join(got, " "), b); n != 1 {
			t.Errorf("%s 出现 %d 次，期望 1（%v）", b, n, got)
		}
	}

	a.quo["sh000001"] = sina.Quote{Amount: 5.5e11}
	a.quo["sz399001"] = sina.Quote{Amount: 6.5e11}
	a.quo["bj899050"] = sina.Quote{Amount: 1e10}
	if got := compact(a.turnover()); got != "12100.00亿" {
		t.Errorf("三市成交 = %s，期望 12100.00亿", got)
	}
}

func TestFlashOnlyOnRealChange(t *testing.T) {
	a := &App{quo: map[string]sina.Quote{}, flash: map[string]flashState{}}
	q := func(last float64) []sina.Quote {
		return []sina.Quote{{Code: "sz300059", Name: "东方财富", Last: last}}
	}

	// 首批快照没有可比对的旧值，不该闪——否则开屏整屏乱闪。
	if a.applyQuotes(q(19.42)) {
		t.Error("首批快照不应触发高亮")
	}
	if a.flashBG("sz300059") != tcell.ColorDefault {
		t.Error("首批快照后不该有底色")
	}

	// 同价重复推送也不该闪。
	if a.applyQuotes(q(19.42)) {
		t.Error("价格未变不应触发高亮")
	}

	if !a.applyQuotes(q(19.45)) {
		t.Fatal("涨价应触发高亮")
	}
	if got := a.flashBG("sz300059"); got != tcell.ColorDarkRed {
		t.Errorf("上涨底色 = %v，期望暗红", got)
	}
	a.applyQuotes(q(19.40))
	if got := a.flashBG("sz300059"); got != tcell.ColorDarkGreen {
		t.Errorf("下跌底色 = %v，期望暗绿", got)
	}

	// 到点后必须自己过期，否则底色会一直挂着。
	a.flash["sz300059"] = flashState{up: true, until: time.Now().Add(-time.Millisecond)}
	if got := a.flashBG("sz300059"); got != tcell.ColorDefault {
		t.Errorf("过期后 = %v，期望恢复默认", got)
	}
}

// 增量帧常常不带名称，合并时必须沿用已有的，否则行情一跳名字就空了。
func TestNameKeptOnPartialFrame(t *testing.T) {
	a := &App{quo: map[string]sina.Quote{}, flash: map[string]flashState{}}
	a.applyQuotes([]sina.Quote{{Code: "sz300059", Name: "东方财富", Last: 19.42}})
	a.applyQuotes([]sina.Quote{{Code: "sz300059", Last: 19.45}})
	if got := a.quo["sz300059"].Name; got != "东方财富" {
		t.Errorf("名称 = %q，期望沿用 东方财富", got)
	}
}

func TestCompactAndPct(t *testing.T) {
	if got := compact(0); got != "-" {
		t.Errorf("compact(0) = %q，期望 -", got)
	}
	if got := compact(1879e4); got != "1879.00万" {
		t.Errorf("compact(1.879e7) = %q，期望 1879.00万", got)
	}
	if got := compact(9.70365152113e11); got != "9703.65亿" {
		t.Errorf("compact(9.7e11) = %q，期望 9703.65亿", got)
	}
	// 涨跌幅为 0 时不该显示成 "-%"。
	if got := pct(0); got != "-" {
		t.Errorf("pct(0) = %q，期望 -", got)
	}
	if got := pct(-0.52); got != "-0.52%" {
		t.Errorf("pct(-0.52) = %q", got)
	}
}

func TestStaleSnapshotDoesNotOverwrite(t *testing.T) {
	a := &App{quo: map[string]sina.Quote{}, flash: map[string]flashState{}}
	push := sina.Quote{Code: "sh600000", Date: "2026-08-31", Time: "09:30:05", Last: 10.5}
	stale := sina.Quote{Code: "sh600000", Date: "2026-08-31", Time: "09:30:01", Last: 10.2}

	a.applyQuotes([]sina.Quote{push})
	if lit := a.applyQuotes([]sina.Quote{stale}); lit {
		t.Error("过期快照不该触发高亮")
	}
	if got := a.quo["sh600000"].Last; got != 10.5 {
		t.Fatalf("过期快照把价格盖回去了: %v", got)
	}
	// 更新的推送照常生效
	a.applyQuotes([]sina.Quote{{Code: "sh600000", Date: "2026-08-31", Time: "09:30:09", Last: 10.8}})
	if got := a.quo["sh600000"].Last; got != 10.8 {
		t.Fatalf("新推送没生效: %v", got)
	}
}

func TestToggleFlash(t *testing.T) {
	a := &App{quo: map[string]sina.Quote{}, flash: map[string]flashState{}}
	q := func(last float64) []sina.Quote {
		return []sina.Quote{{Code: "sz300059", Last: last}}
	}
	a.applyQuotes(q(19.42))
	a.applyQuotes(q(19.45)) // 亮起来

	a.noFlash = true
	clear(a.flash) // toggleFlash 里做的事，这里不碰 tview
	if a.applyQuotes(q(19.50)) {
		t.Error("关闭后不该再触发高亮")
	}
	if a.flashBG("sz300059") != tcell.ColorDefault {
		t.Error("关闭后不该还有底色")
	}
	if got := a.quo["sz300059"].Last; got != 19.50 {
		t.Fatalf("关闭闪烁不该影响价格更新: %v", got)
	}

	a.noFlash = false
	if !a.applyQuotes(q(19.60)) {
		t.Error("重新开启后应能高亮")
	}
}

func TestAlertFiresOnceAndNotifies(t *testing.T) {
	old := notify
	var got []string
	notify = func(title, text string) { got = append(got, title+" | "+text) }
	defer func() { notify = old }()

	saved := 0
	a := &App{
		quo:   map[string]sina.Quote{},
		flash: map[string]flashState{},
		save:  func([]Item) { saved++ },
		items: []Item{
			{Code: "sh508000", Name: "张江REIT", Alert: 2.000, AlertUp: true},
			{Code: "sz300059", Name: "东方财富", Alert: 19.000, AlertUp: false},
		},
		status: newStatusView(),
	}

	// 都没到线
	a.applyQuotes([]sina.Quote{{Code: "sh508000", Last: 1.990}, {Code: "sz300059", Last: 19.400}})
	if a.checkAlerts() || len(got) != 0 {
		t.Fatal("没到价不该触发")
	}

	// 涨到 2.001：触发一次
	a.applyQuotes([]sina.Quote{{Code: "sh508000", Last: 2.001}})
	if !a.checkAlerts() {
		t.Fatal("涨到提醒价应触发")
	}
	if len(got) != 1 || !strings.Contains(got[0], "涨到") {
		t.Fatalf("通知内容 = %v", got)
	}
	if a.items[0].Alert != 0 {
		t.Error("触发后应清掉提醒，否则每帧都弹")
	}
	// 后续行情不再重复通知
	a.applyQuotes([]sina.Quote{{Code: "sh508000", Last: 2.100}})
	if a.checkAlerts() || len(got) != 1 {
		t.Fatalf("提醒应是一次性的，got %v", got)
	}

	// 跌破方向
	a.applyQuotes([]sina.Quote{{Code: "sz300059", Last: 18.990}})
	if !a.checkAlerts() || len(got) != 2 || !strings.Contains(got[1], "跌到") {
		t.Fatalf("跌到提醒价应触发，got %v", got)
	}
}

// 品种名来自新浪，直接拼进 AppleScript 是命令注入面。
func TestQuoteASEscapes(t *testing.T) {
	if got := quoteAS(`他说"跌了"`); got != `"他说\"跌了\""` {
		t.Errorf("引号未转义: %s", got)
	}
	if got := quoteAS("a\nb"); strings.Contains(got, "\n") {
		t.Errorf("换行应被替换掉: %q", got)
	}
	if got := quoteAS(`c:\x`); got != `"c:\\x"` {
		t.Errorf("反斜杠未转义: %s", got)
	}
}

// setStatus 会写 tview 控件，测试里给个真控件即可，不必起整个 App。
func newStatusView() *tview.TextView { return tview.NewTextView() }

func TestParseTargetPercentAndPrice(t *testing.T) {
	q := sina.Quote{Prev: 2.000, Last: 1.992}
	cases := []struct {
		in   string
		want float64
	}{
		{"", 0},          // 取消
		{"2.09", 2.09},   // 绝对价
		{"+5%", 2.100},   // 以昨收为基准，不是现价
		{"5%", 2.100},    // 正号可省
		{"-2.5%", 1.950}, //
		{" 3 % ", 2.060}, // 空格容错
		{"0", 0},         // 显式取消
	}
	for _, c := range cases {
		got, err := parseTarget(c.in, q)
		if err != nil {
			t.Errorf("parseTarget(%q) 报错: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseTarget(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	// 昨收缺失时退回现价
	if got, _ := parseTarget("+10%", sina.Quote{Last: 100}); got != 110 {
		t.Errorf("无昨收应按现价算, got %v", got)
	}
	for _, bad := range []string{"abc", "5%%", "-3", "%"} {
		if _, err := parseTarget(bad, q); err == nil {
			t.Errorf("parseTarget(%q) 应报错", bad)
		}
	}
}

// 已设提醒时再按 n，输入框要带出原值（清空回车即取消），而不是又填回现价。
func TestAlertDialogPrefillsExisting(t *testing.T) {
	a := New(sina.NewFeed(),
		[]Item{{Code: "sh508000", Name: "张江REIT", Alert: 2.100, AlertUp: true}},
		func([]Item) {})
	a.quo["sh508000"] = sina.Quote{Code: "sh508000", Last: 1.992, Prev: 2.0}
	a.table.Select(1, 0)

	a.showAlert()
	in, ok := findInput(a.pages)
	if !ok {
		t.Fatal("没弹出设价框")
	}
	if in.GetText() != "2.100" {
		t.Fatalf("输入框预填 %q，应带出已设的 2.100", in.GetText())
	}

	// 清空回车 = 取消
	a.setAlert("sh508000", "", a.quo["sh508000"])
	if a.items[0].Alert != 0 {
		t.Fatal("清空后应取消提醒")
	}
}

func findInput(p *tview.Pages) (*tview.InputField, bool) {
	_, page := p.GetFrontPage()
	var found *tview.InputField
	var walk func(tview.Primitive)
	walk = func(prim tview.Primitive) {
		switch v := prim.(type) {
		case *tview.InputField:
			found = v
		case *tview.Flex:
			for i := 0; i < v.GetItemCount(); i++ {
				walk(v.GetItem(i))
			}
		}
	}
	walk(page)
	return found, found != nil
}
