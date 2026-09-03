package ui

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"sinago/sina"
)

// fired 判断当前价是否触碰提醒线。方向在设置时就按当时价位定死（目标价高于
// 现价就是「涨到」，反之是「跌到」），所以这里不需要上一笔价格，也就不会在
// 漏帧或重连补快照时错过一次穿越。
func (it Item) fired(last float64) bool {
	if it.Alert == 0 || last == 0 {
		return false
	}
	if it.AlertUp {
		return last >= it.Alert
	}
	return last <= it.Alert
}

func (it Item) alertText() string {
	if it.Alert == 0 {
		return ""
	}
	dir := "跌到"
	if it.AlertUp {
		dir = "涨到"
	}
	return fmt.Sprintf("%s %s", dir, price(it.Alert))
}

// checkAlerts 在行情更新后逐条检查提醒，触发的发系统通知并清掉（一次性）。
// 返回是否有变动——有的话调用方要落盘。
func (a *App) checkAlerts() bool {
	hit := false
	for i, it := range a.items {
		q, ok := a.quo[it.Code]
		if !ok || !it.fired(q.Last) {
			continue
		}
		a.items[i].Alert, a.items[i].AlertUp = 0, false
		hit = true
		notify(it.Name+" "+it.alertText(),
			fmt.Sprintf("现价 %s（%s）", price(q.Last), it.Code))
		a.setStatus(fmt.Sprintf("⏰ %s %s，现价 %s", it.Name, it.alertText(), price(q.Last)))
	}
	return hit
}

// notify 发一条 macOS 系统通知。osascript 是系统自带的，不必引第三方通知库；
// 通知会挂在「脚本编辑器」名下（首次需要在系统设置里允许它发通知）。
// 异步执行：osascript 冷启动要一两百毫秒，别卡住 UI 线程。
var notify = func(title, text string) {
	go exec.Command("osascript", "-e", fmt.Sprintf(
		`display notification %s with title %s sound name "Glass"`,
		quoteAS(text), quoteAS(title))).Run()
}

// quoteAS 把字符串包成 AppleScript 字面量。品种名来自新浪，虽然目前只有中文和
// 括号，但引号/反斜杠/换行不转义就能改写脚本语义，这里是命令拼接的边界。
func quoteAS(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\r", " ").Replace(s)
	return `"` + s + `"`
}

// showAlert 弹出设价框。已设提醒的品种再按一次 n 就能看到原值并改或清掉。
func (a *App) showAlert() {
	it, ok := a.selected()
	if !ok {
		return
	}
	q, has := a.quo[it.Code]
	if !has || q.Last == 0 {
		a.setStatus("还没有行情，等价格出来再设提醒")
		return
	}

	// 已有提醒时把原值填进去：既看得见当前设的是多少，清空回车就是取消——
	// 不然「怎么撤掉」得靠猜。没提醒时填现价，改两位数就是个新提醒。
	text, title := price(q.Last), " 到价提醒：填价格 2.09，或涨跌幅 +5% / -2%（留空取消） "
	if it.Alert != 0 {
		text = price(it.Alert)
		title = fmt.Sprintf(" 到价提醒：当前 %s —— 改价，或清空回车取消 ", it.alertText())
	}
	input := tview.NewInputField().
		SetLabel(fmt.Sprintf(" %s 现价 %s (%s)  ", it.Name, price(q.Last), pct(q.ChangePct))).
		SetText(text)
	input.SetDoneFunc(func(key tcell.Key) {
		a.pages.RemovePage("alert")
		a.app.SetFocus(a.table)
		if key != tcell.KeyEnter {
			return
		}
		a.setAlert(it.Code, input.GetText(), q)
	})

	box := tview.NewFlex().AddItem(input, 0, 1, true)
	box.SetBorder(true).SetTitle(title)
	a.pages.AddPage("alert", center(box, 60, 3), true, true)
	a.app.SetFocus(input)
}

// parseTarget 把输入解析成目标价。除了绝对价（2.09），也收涨跌幅（+5%、-2%、5%），
// 省得自己拿计算器按——盯盘时想的是「涨 5% 提醒我」，不是「涨到 2.0916 提醒我」。
// 百分比以昨收为基准，跟表里「涨跌幅」列同一口径：填 +5% 就是那一列走到 +5.00%。
// 昨收缺失的品种（少数）退回按现价算。返回 0 表示取消提醒。
func parseTarget(text string, q sina.Quote) (float64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	if body, ok := strings.CutSuffix(text, "%"); ok {
		p, err := strconv.ParseFloat(strings.TrimSpace(body), 64)
		if err != nil {
			return 0, fmt.Errorf("涨跌幅看不懂: %s", text)
		}
		base := q.Prev
		if base == 0 {
			base = q.Last
		}
		// 价格只显示到 3 位小数，这里就取整到 3 位，免得提醒线卡在显示不出来的
		// 第 4 位上——看着已经到价了却不响，最难解释。
		return math.Round(base*(1+p/100)*1000) / 1000, nil
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("提醒价看不懂: %s", text)
	}
	return v, nil
}

func (a *App) setAlert(code, text string, q sina.Quote) {
	for i, it := range a.items {
		if it.Code != code {
			continue
		}
		v, err := parseTarget(text, q)
		if err != nil {
			a.setStatus(err.Error())
			return
		}
		if v == 0 {
			a.items[i].Alert, a.items[i].AlertUp = 0, false
			a.save(a.items)
			a.redraw()
			a.setStatus("已取消 " + it.Name + " 的提醒")
			return
		}
		a.items[i].Alert, a.items[i].AlertUp = v, v > q.Last
		a.save(a.items)
		a.redraw()
		a.setStatus(fmt.Sprintf("已设提醒 %s %s（当前 %s%s）",
			it.Name, a.items[i].alertText(), price(q.Last), pctFrom(q.Prev, v)))
		return
	}
}

// pctFrom 把目标价换算回涨跌幅，好让「填价格」和「填涨跌幅」两种设法都能
// 当场看到另一种口径。
func pctFrom(prev, target float64) string {
	if prev == 0 {
		return ""
	}
	return "，目标 " + pct((target-prev)/prev*100)
}
