package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"sinago/sina"
)

// 涨红跌绿，A股口径。
const (
	colUp   = tcell.ColorRed
	colDown = tcell.ColorGreen
	colFlat = tcell.ColorSilver
)

var columns = []string{
	"代码", "名称", "最新价", "涨跌幅", "涨跌额", "均价",
	"成交量", "成交额", "最高", "最低", "今开", "昨收", "时间",
}

// 每列的固定显示宽度。tview 按当列最宽的单元格算列宽，价格从 9.99 跳到
// 10.00、成交量从「万」进到「亿」都会让后面所有列横向平移一格——比高亮
// 本身更晃眼。补齐到定宽后列位不再随行情动。
var widths = []int{10, 16, 9, 8, 9, 9, 10, 10, 9, 9, 9, 9, 8}

// 三市指数：无论自选里有没有，都常驻订阅，底栏的三市成交额靠它们算。
// 用户自己加了就照常在表里显示，没加就只在后台拉数据。
var baseCodes = []string{"sh000001", "sz399001", "bj899050"}

// 价格变动后高亮多久。
const flashFor = 400 * time.Millisecond

// 重绘节流间隔。行情帧到达是突发的（一秒可能十几帧），逐帧重绘就是满屏抖。
// 终端行情软件的通行做法是把这段时间内的跳动合并成一次刷新。
const drawEvery = 120 * time.Millisecond

// Item 是一条自选，持久化到 watchlist.json。
type Item struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// 到价提醒。方向在设置时按当时价位定死，触发后清零（一次性）。
	Alert   float64 `json:"alert,omitempty"`
	AlertUp bool    `json:"alert_up,omitempty"`
}

// App 是整个 TUI。
type App struct {
	app    *tview.Application
	pages  *tview.Pages
	table  *tview.Table
	detail *tview.TextView
	status *tview.TextView
	body   *tview.Flex

	feed  *sina.Feed
	items []Item
	quo   map[string]sina.Quote
	save  func([]Item)

	flash    map[string]flashState // 代码 -> 最近一次价格变动
	showInfo bool                  // 详情面板是否可见
	noFlash  bool                  // 关掉价格变动闪烁（默认开）

	msg      string // 底栏最近一条消息
	dirty    bool   // 有新行情待画
	flashLit bool // 上一帧画出过高亮，得再画一次把它抹掉
}

type flashState struct {
	up    bool
	until time.Time
}

func New(feed *sina.Feed, items []Item, save func([]Item)) *App {
	a := &App{
		app:   tview.NewApplication(),
		feed:  feed,
		items: items,
		quo:   map[string]sina.Quote{},
		save:  save,
		flash: map[string]flashState{},
	}

	a.table = tview.NewTable().SetFixed(1, 2).SetSelectable(true, false)
	a.table.SetBorder(true).SetTitle(" 自选 ")

	a.detail = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.detail.SetBorder(true).SetTitle(" 详情 ")

	a.status = tview.NewTextView().SetDynamicColors(true)
	a.setStatus("")

	a.body = tview.NewFlex()
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.body, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.layout()

	a.pages = tview.NewPages().AddPage("main", root, true, true)
	a.table.SetSelectionChangedFunc(func(int, int) { a.renderDetail() })
	a.app.SetInputCapture(a.keys)
	a.redraw()
	return a
}

func (a *App) Run(ctx context.Context) error {
	go a.pump(ctx)
	a.feed.Set(a.codes())
	return a.app.SetRoot(a.pages, true).EnableMouse(true).Run()
}

// pump 把行情和状态搬到 UI 线程。合并数据（QueueUpdate，不画）和重绘
// （定时器）分开：来多少帧都只按 drawEvery 的节奏刷屏。
func (a *App) pump(ctx context.Context) {
	tick := time.NewTicker(drawEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case qs := <-a.feed.Out:
			a.app.QueueUpdate(func() {
				a.applyQuotes(qs)
				if a.checkAlerts() {
					a.save(a.items)
				}
				a.dirty = true
			})
		case <-tick.C:
			a.app.QueueUpdate(func() {
				// flashLit：高亮到点后即使没有新行情，也得再画一次把底色抹掉。
				if !a.dirty && !a.flashLit {
					return
				}
				a.redraw()
				// 必须是 ForceDraw：Draw() 自己就是一次 QueueUpdate，
				// 在已排队的更新里调用会等一个永远轮不到的回执，整个界面卡死。
				a.app.ForceDraw()
			})
		case s := <-a.feed.Status:
			a.app.QueueUpdateDraw(func() { a.setStatus(s) })
		}
	}
}

// layout 按 showInfo 重排主区。12 列在带详情面板时放不下，
// 默认收起，Enter 才展开——先看全整张自选表，要细节再点开。
func (a *App) layout() {
	a.body.Clear().AddItem(a.table, 0, 3, true)
	if a.showInfo {
		a.body.AddItem(a.detail, 44, 0, false)
	}
}

// applyQuotes 合并一批行情并登记价格变动，返回是否有行需要高亮。
func (a *App) applyQuotes(qs []sina.Quote) bool {
	lit := false
	for _, q := range qs {
		old, had := a.quo[q.Code]
		// 增量帧可能只带部分字段，名称为空时沿用已有的。
		if q.Name == "" && had {
			q.Name = old.Name
		}
		// 快照和 WSS 推送是并发的，慢一步到的快照不能把更新的推送盖回去
		// （会画出一个方向相反的高亮，价格也倒退一格）。
		if had && q.Date == old.Date && q.Time != "" && q.Time < old.Time {
			continue
		}
		// 首批快照不算变动，否则开屏整屏乱闪。
		if had && old.Last != 0 && q.Last != old.Last {
			a.flash[q.Code] = flashState{up: q.Last > old.Last, until: time.Now().Add(flashFor)}
			lit = lit || !a.noFlash
		}
		a.quo[q.Code] = q
	}
	return lit
}

func (a *App) setStatus(s string) {
	a.msg = s
	a.drawStatus()
}

// drawStatus 画底栏：快捷键 + 三市成交额 + 最近一条消息。成交额随行情走，
// 所以每帧都要重画，不能只在 setStatus 时更新。
func (a *App) drawStatus() {
	amt := ""
	if v := a.turnover(); v > 0 {
		amt = "  [yellow]三市成交 " + compact(v)
	}
	a.status.SetText(fmt.Sprintf(
		"[gray]a 添加  d 删除  J/K 调序  Enter 详情  n 提醒  f 闪烁  r 刷新  q 退出%s   [white]%s",
		amt, a.msg))
}

// turnover 是沪深北三个指数成交额之和。指数报文里的成交额就是该市场全场
// 的量，直接相加即当日三市总额。
func (a *App) turnover() float64 {
	var sum float64
	for _, c := range baseCodes {
		sum += a.quo[c].Amount
	}
	return sum
}

func (a *App) codes() []string {
	c := make([]string, 0, len(a.items)+len(baseCodes))
	for _, it := range a.items {
		c = append(c, it.Code)
	}
	for _, b := range baseCodes {
		if !slices.Contains(c, b) {
			c = append(c, b)
		}
	}
	return c
}

func (a *App) redraw() {
	a.dirty, a.flashLit = false, false
	a.drawStatus()
	row, _ := a.table.GetSelection()
	a.table.Clear()
	for i, h := range columns {
		align := tview.AlignRight
		if i < 2 {
			align = tview.AlignLeft
		}
		a.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).SetAlign(align).SetSelectable(false))
	}
	for i, it := range a.items {
		q, ok := a.quo[it.Code]
		if !ok {
			q = sina.Quote{Code: it.Code, Name: it.Name}
		}
		name := q.Name
		if name == "" {
			name = it.Name
		}
		if it.Alert != 0 {
			name = "⏰" + name // 详情面板默认收起，提醒只能挂在主表上才看得见
		}
		c := color(q.Change)
		cells := []struct {
			s string
			c tcell.Color
		}{
			{it.Code, tcell.ColorSilver},
			{name, tcell.ColorWhite},
			{price(q.Last), c},
			{pct(q.ChangePct), c},
			{signed(q.Change), c},
			{price(q.AvgPrice()), colFlat},
			{compact(q.Volume), colFlat},
			{compact(q.Amount), colFlat},
			{price(q.High), colFlat},
			{price(q.Low), colFlat},
			{price(q.Open), colFlat},
			{price(q.Prev), colFlat},
			{q.Time, colFlat},
		}
		bg := a.flashBG(it.Code)
		for j, cell := range cells {
			align, s := tview.AlignRight, padL(cell.s, widths[j])
			if j < 2 {
				// 名称不封顶的话「比特币美元指数(BTC/USD)」这类长名会把末尾的时间列挤没。
				align, s = tview.AlignLeft, pad(cell.s, widths[j])
			}
			tc := tview.NewTableCell(s).SetTextColor(cell.c).SetAlign(align)
			// 只闪跳动的那一格，不整行反白：整行一亮，眼睛既看不出是哪个字段
			// 动了，十三列同时变色本身也比行情更吵。
			if bg != tcell.ColorDefault && j == 2 {
				tc.SetBackgroundColor(bg).SetTextColor(tcell.ColorWhite)
			}
			a.table.SetCell(i+1, j, tc)
		}
	}
	if row > 0 && row <= len(a.items) {
		a.table.Select(row, 0)
	} else if len(a.items) > 0 {
		a.table.Select(1, 0)
	}
	a.renderDetail()
}

// renderDetail 展示当前品种的全部原始字段。各品种字段差异极大——
// A股有五档、美股有市盈率、期货有持仓量——塞进主表只会得到一堆空列。
func (a *App) renderDetail() {
	if !a.showInfo {
		return // 收起时每帧重写一遍看不见的面板，纯浪费
	}
	a.detail.Clear()
	it, ok := a.selected()
	if !ok {
		return
	}
	q, ok := a.quo[it.Code]
	if !ok {
		fmt.Fprintf(a.detail, "[gray]%s 等待行情…", it.Code)
		return
	}
	fmt.Fprintf(a.detail, "[yellow]%s [white]%s  [gray]%s\n\n", q.Name, q.Code, q.Kind)

	// 指数报文的五档全是 0，占半屏却什么也没说，直接不画。
	if hasDepth(q) {
		fmt.Fprint(a.detail, "[yellow]盘口\n")
		for i := len(q.Asks) - 1; i >= 0; i-- {
			fmt.Fprintf(a.detail, "  卖%d  [green]%9s[white] %10s\n",
				i+1, price(q.Asks[i].Price), compact(q.Asks[i].Vol))
		}
		for i := range q.Bids {
			fmt.Fprintf(a.detail, "  买%d  [red]%9s[white] %10s\n",
				i+1, price(q.Bids[i].Price), compact(q.Bids[i].Vol))
		}
		fmt.Fprintln(a.detail)
	}

	fmt.Fprint(a.detail, "[yellow]字段\n")
	for _, f := range q.Fields {
		// fN 是新浪未公开含义的位置，原样列出但标灰，不假装读得懂。
		if strings.TrimSpace(f.V) == "" || strings.HasPrefix(f.K, "买") || strings.HasPrefix(f.K, "卖") {
			continue
		}
		c := "white"
		if strings.HasPrefix(f.K, "f") {
			c = "gray"
		}
		fmt.Fprintf(a.detail, "  [%s]%-10s %s\n", c, f.K, f.V)
	}
}

// flashBG 返回该代码当前的高亮底色，过期、关闭或无变动时返回 ColorDefault。
func (a *App) flashBG(code string) tcell.Color {
	f, ok := a.flash[code]
	if !ok || a.noFlash {
		return tcell.ColorDefault
	}
	if time.Now().After(f.until) {
		delete(a.flash, code)
		return tcell.ColorDefault
	}
	a.flashLit = true
	if f.up {
		return tcell.ColorDarkRed
	}
	return tcell.ColorDarkGreen
}

func hasDepth(q sina.Quote) bool {
	for _, l := range append(append([]sina.Level{}, q.Bids...), q.Asks...) {
		if l.Price != 0 {
			return true
		}
	}
	return false
}

func (a *App) selected() (Item, bool) {
	row, _ := a.table.GetSelection()
	if row < 1 || row > len(a.items) {
		return Item{}, false
	}
	return a.items[row-1], true
}

func (a *App) keys(ev *tcell.EventKey) *tcell.EventKey {
	if front, _ := a.pages.GetFrontPage(); front != "main" {
		return ev // 弹层自己处理按键
	}
	if ev.Key() == tcell.KeyEnter {
		a.showInfo = !a.showInfo
		a.layout()
		a.renderDetail()
		return nil
	}
	switch ev.Rune() {
	case 'q':
		a.app.Stop()
		return nil
	case 'a':
		a.showSearch()
		return nil
	case 'd':
		a.removeSelected()
		return nil
	case 'n':
		a.showAlert()
		return nil
	case 'f':
		a.toggleFlash()
		return nil
	case 'r':
		a.feed.Set(a.codes()) // 重连并重拉快照
		return nil
	case 'J':
		a.move(1)
		return nil
	case 'K':
		a.move(-1)
		return nil
	}
	return ev
}

// toggleFlash 开关价格变动闪烁。盯盘久了满屏跳色很累，
// 关掉后已经亮着的也要立刻抹掉，否则最后一次高亮会一直挂在那。
func (a *App) toggleFlash() {
	a.noFlash = !a.noFlash
	clear(a.flash)
	a.redraw()
	if a.noFlash {
		a.setStatus("已关闭价格闪烁")
	} else {
		a.setStatus("已开启价格闪烁")
	}
}

func (a *App) move(d int) {
	row, _ := a.table.GetSelection()
	i := row - 1
	j := i + d
	if i < 0 || i >= len(a.items) || j < 0 || j >= len(a.items) {
		return
	}
	a.items[i], a.items[j] = a.items[j], a.items[i]
	a.save(a.items)
	a.redraw()
	a.table.Select(j+1, 0)
}

// removeSelected 先问一句再删。删除会立刻落盘，手滑一下就少一条自选，
// 而删掉的名字未必还记得——确认弹层比事后从备份里翻便宜。
func (a *App) removeSelected() {
	it, ok := a.selected()
	if !ok {
		return
	}
	m := tview.NewModal().
		SetText(fmt.Sprintf("删除自选 %s（%s）？", it.Name, it.Code)).
		AddButtons([]string{"取消", "删除"}).
		SetDoneFunc(func(i int, _ string) {
			a.pages.RemovePage("confirm")
			a.app.SetFocus(a.table)
			if i == 1 {
				a.remove(it.Code)
			}
		})
	a.pages.AddPage("confirm", m, true, true)
	a.app.SetFocus(m)
}

// remove 按代码删，不按行号——确认期间行情刷新过，选中行未必还是当初那条。
func (a *App) remove(code string) {
	for i, it := range a.items {
		if it.Code != code {
			continue
		}
		a.items = append(a.items[:i], a.items[i+1:]...)
		if !slices.Contains(baseCodes, code) {
			delete(a.quo, code) // 三市指数删出自选后仍在后台订阅，行情不能丢
		}
		a.save(a.items)
		a.redraw()
		a.feed.Set(a.codes())
		a.setStatus("已删除 " + it.Name)
		return
	}
}

// add 先实拉一次确认有行情再落库。suggest 能搜到但 hq 没有数据源的品种
// （美元指数、期权…）会当场报错并留在弹层里，而不是往表里加一行永远空白。
func (a *App) add(code, name string) {
	for _, it := range a.items {
		if it.Code == code {
			a.setStatus(name + " 已在自选中")
			a.closeSearch()
			return
		}
	}
	a.setStatus("正在验证 " + code + " …")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		q, err := sina.Verify(ctx, code)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.setStatus("添加失败: " + err.Error())
				return // 弹层保持打开，让用户改选
			}
			if q.Name != "" {
				name = q.Name // 用行情里的正式名称，比 suggest 的更准
			}
			a.items = append(a.items, Item{Code: code, Name: name})
			a.quo[code] = q
			a.save(a.items)
			a.redraw()
			a.feed.Set(a.codes())
			a.setStatus("已添加 " + name)
			a.closeSearch()
		})
	}()
}

func (a *App) showSearch() {
	input := tview.NewInputField().SetLabel(" 搜索 ")
	// 单行显示：双行版一屏只放得下六七条，合成一行能看两倍。
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true).SetTitle(" 结果 (Enter 添加) ")

	// 防抖：不加的话「腾讯控股」四个字会打四次请求，且结果列表边打边跳。
	var debounce *time.Timer
	input.SetChangedFunc(func(text string) {
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(250*time.Millisecond, func() {
			hits, err := sina.Search(context.Background(), text)
			a.app.QueueUpdateDraw(func() {
				if input.GetText() != text {
					return // 已有更新的输入，丢弃这次结果
				}
				a.fillResults(list, text, hits, err)
			})
		})
	})

	// 输入框里按 ↓ 或 Enter 跳到结果列表，Esc 关闭。
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			a.closeSearch()
			return nil
		case tcell.KeyDown, tcell.KeyEnter:
			a.app.SetFocus(list)
			return nil
		}
		return ev
	})
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			a.closeSearch()
			return nil
		case tcell.KeyUp:
			if list.GetCurrentItem() == 0 {
				a.app.SetFocus(input)
				return nil
			}
		}
		return ev
	})

	box := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false)
	box.SetBorder(true).SetTitle(" 添加自选 (Esc 取消) ")

	a.pages.AddPage("search", center(box, 70, 20), true, true)
	a.app.SetFocus(input)
}

// fillResults 把搜索结果画成单行列表：`名称 · 分类 · 代码`，用颜色区分列。
func (a *App) fillResults(list *tview.List, text string, hits []sina.Hit, err error) {
	list.Clear()
	if err != nil {
		list.AddItem("搜索失败: "+err.Error(), "", 0, nil)
		return
	}
	for _, h := range hits {
		h := h
		if h.Code == "" {
			list.AddItem("[gray]"+pad(h.Name, 24)+h.Why, "", 0, nil)
			continue
		}
		list.AddItem(pad(h.Name, 24)+"[aqua]"+pad(string(h.Kind), 10)+"[gray]"+h.Code,
			"", 0, func() { a.add(h.Code, h.Name) })
	}
	// 支持直接粘代码：输入形如 sh600519 / si932000 的串时不必等搜索命中。
	// 放在首屏最后一行——有搜索结果时它是兜底，不该抢占第一行；没结果时自然落到第一行。
	if raw := sina.NormalizeCode(text); raw != "" && looksLikeCode(raw) {
		list.InsertItem(lastRowOfFirstPage(list), pad(raw, 24)+"[gray]直接使用该代码", "", 0,
			func() { a.add(raw, raw) })
	}
}

// lastRowOfFirstPage 返回首屏最后一行的下标；列表还没布局时按 15 行估。
func lastRowOfFirstPage(list *tview.List) int {
	_, _, _, h := list.GetInnerRect()
	if h <= 0 {
		h = 15
	}
	if n := list.GetItemCount(); n < h {
		return n
	}
	return h - 1
}

// pad 按终端显示宽度截断并补齐——中文占两格，用 %-24s 补出来的列是歪的。
func pad(s string, w int) string {
	if n := tview.TaggedStringWidth(s); n <= w {
		return s + strings.Repeat(" ", w-n)
	}
	var b strings.Builder
	for _, r := range s {
		if tview.TaggedStringWidth(b.String()+string(r)) > w-1 {
			break
		}
		b.WriteRune(r)
	}
	out := b.String() + "…"
	return out + strings.Repeat(" ", w-tview.TaggedStringWidth(out))
}

// padL 是 pad 的右对齐版，给数字列定宽用。
func padL(s string, w int) string {
	if n := tview.TaggedStringWidth(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

func (a *App) closeSearch() {
	a.pages.RemovePage("search")
	a.app.SetFocus(a.table)
}

// looksLikeCode 判断输入是否可能是 sinajs 代码，是则允许跳过搜索直接添加。
// 不再维护前缀白名单——它总是漏掉新前缀（si_ 中证指数、lse_、globalbd_），
// 而 add() 本来就会实拉验证，拉不到会报错，放宽到「纯 ASCII 代码字符」即可。
func looksLikeCode(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '$' || r == '.') {
			return false
		}
	}
	return true
}

func center(p tview.Primitive, w, h int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, h, 0, true).
			AddItem(nil, 0, 1, false), w, 0, true).
		AddItem(nil, 0, 1, false)
}

func color(v float64) tcell.Color {
	switch {
	case v > 0:
		return colUp
	case v < 0:
		return colDown
	}
	return colFlat
}

func price(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.3f", v)
}

func signed(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%+.3f", v)
}

func pct(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%+.2f%%", v)
}

// compact 把成交量压成万/亿，否则一列十位数会把表撑爆。
func compact(v float64) string {
	switch {
	case v == 0:
		return "-"
	case v >= 1e8:
		return fmt.Sprintf("%.2f亿", v/1e8)
	case v >= 1e4:
		return fmt.Sprintf("%.2f万", v/1e4)
	}
	return fmt.Sprintf("%.0f", v)
}
