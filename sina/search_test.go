package sina

import (
	"context"
	"strings"
	"testing"
	"time"
)

const suggestFixture = `var suggestvalue="恒生指数,33,hsi,hsi,恒生指数,,恒生指数,99,1,,,;恒生指数期货,86,hsi,hsi,恒生指数期货,,恒生指数期货,99,1,,,;沪深300,87,if0,if0,沪深300,,沪深300,99,1,,,;沪深300,11,000300,sh000300,沪深300,,沪深300,99,1,,,;腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,,;苹果,41,aapl,aapl,苹果,,苹果,99,1,ESG,,;纳斯达克,41,.ixic,.ixic,纳斯达克,,纳斯达克,99,1,,,;美元人民币,71,usdcny,usdcny,美元人民币,,美元人民币,99,1,,,;以太坊美元,71,btcethusd,btcethusd,以太坊美元,,以太坊美元,99,1,,,;上证50ETF华夏,203,510050,sh510050,上证50ETF华夏,,上证50ETF华夏,99,1,,,;上证50ETF华夏,22,510050,of510050,上证50ETF华夏,,上证50ETF华夏,99,1,,,;螺纹钢2610购3500,109,rb2610C3500,rb2610C3500,螺纹钢2610购3500,,螺纹钢2610购3500,99,1,,,";`

func TestSuggestTypeMapping(t *testing.T) {
	want := map[string]struct {
		code string
		kind Kind
	}{
		"恒生指数":   {"rt_hkHSI", KindHK},
		"恒生指数期货": {"hf_HSI", KindHF},
		"沪深300":  {"nf_IF0", KindNF}, // 87 先出现，去重后保留期货那条
		"腾讯控股":   {"rt_hk00700", KindHK},
		"苹果":     {"gb_aapl", KindUS},
		"纳斯达克":   {"gb_$ixic", KindUS}, // 点号必须换成 $，否则拉不到数据
		"美元人民币":  {"fx_susdcny", KindFX},
		"以太坊美元":  {"btc_btcethusd", KindCrypto}, // 与外汇同为 71，靠 btc 前缀分流
	}
	byName := map[string]Hit{}
	for _, h := range parseSuggest(suggestFixture) {
		if _, dup := byName[h.Name]; !dup {
			byName[h.Name] = h
		}
	}
	for name, w := range want {
		h, ok := byName[name]
		if !ok {
			t.Errorf("%s: 未出现在结果中", name)
			continue
		}
		if h.Code != w.code || h.Kind != w.kind {
			t.Errorf("%s: 得到 %s/%s，期望 %s/%s", name, h.Code, h.Kind, w.code, w.kind)
		}
	}
	// 期权没有行情源，必须明确标为不支持而不是拼一个拉不到数据的代码。
	if h := byName["螺纹钢2610购3500"]; h.Code != "" || h.Why == "" {
		t.Errorf("期权应判为不支持，得到 code=%q why=%q", h.Code, h.Why)
	}
	// 同名不同源（场内 sh510050 / 场外 of510050）应各占一行，不能被去重掉。
	var etf []string
	for _, h := range parseSuggest(suggestFixture) {
		if h.Name == "上证50ETF华夏" {
			etf = append(etf, h.Code)
		}
	}
	if len(etf) != 2 || etf[0] != "sh510050" || etf[1] != "of510050" {
		t.Errorf("场内/场外应各保留一条，得到 %v", etf)
	}
}

// TestLiveTypeMappingHasData 用真实接口验证每类映射确实拉得到数据。
// 布局或代码规则一旦变动，这里会先于 UI 报错。跑 -short 可跳过。
func TestLiveTypeMappingHasData(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过实网测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	codes := []string{
		"sh600519", "sz300059", "sh000001", "sh510050", "of510050",
		"rt_hk00700", "rt_hkHSI", "gb_aapl", "gb_$ixic", "int_hangseng",
		"fx_susdcny", "btc_btcbtcusd", "nf_IF0", "nf_RB0", "hf_CL", "hf_GC",
	}
	qs, err := Snapshot(ctx, codes)
	if err != nil {
		t.Fatalf("快照失败: %v", err)
	}
	got := map[string]Quote{}
	for _, q := range qs {
		got[q.Code] = q
	}
	for _, c := range codes {
		q, ok := got[c]
		if !ok {
			t.Errorf("%s: 无数据，映射规则可能已失效", c)
			continue
		}
		if q.Name == "" || q.Last == 0 {
			t.Errorf("%s: 名称=%q 现价=%v，字段错位", c, q.Name, q.Last)
		}
	}
}

// 美元指数在 suggest 里搜得到（type 71），但 hq 任何前缀都拉不到数据。
// 映射表覆盖不了这种边角，只能靠添加前实拉一次挡住。
func TestVerifyRejectsCodeWithoutData(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过实网测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if q, err := Verify(ctx, "fx_susdcny"); err != nil || q.Last == 0 {
		t.Errorf("有效代码应通过验证: q=%+v err=%v", q, err)
	}
	if _, err := Verify(ctx, "fx_sdiniw"); err == nil {
		t.Error("美元指数无行情源，应被拒绝而不是加成一行空白")
	}
}

func TestSearchLive(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过实网测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	hits, err := Search(ctx, "腾讯")
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	// 中文查询走 UTF-8；若误用 GBK，接口会返回一串与关键词无关的热门股。
	var hit bool
	for _, h := range hits {
		if strings.Contains(h.Name, "腾讯") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("中文搜索未命中，查询编码可能不对；返回 %d 条: %+v", len(hits), hits)
	}
}

func TestBondSuggestType214(t *testing.T) {
	// 真实 suggest 条目（2026-08-31 抓取）
	body := `var suggestvalue="美国10年期国债,214,us10yt,us10yt,美国10年期国债,,美国10年期国债,99,1,,,";`
	hits := parseSuggest(body)
	if len(hits) != 1 || hits[0].Code != "globalbd_us10yt" || hits[0].Kind != KindBond {
		t.Fatalf("got %+v", hits)
	}
}

// REITs 的 suggest 全码是 cf 打头的自造前缀，hq 完全不认，必须按代码段还原
// 成 sh/sz 全码（真实条目，2026-08-31 抓取）。
func TestREITsSuggestType26(t *testing.T) {
	body := `var suggestvalue="华安张江产业园REIT,26,508000,cf508000,华安张江产业园REIT,,,99,1,,,;` +
		`博时招商蛇口产业园REIT,26,180101,cf180101,博时招商蛇口产业园REIT,,,99,1,,,";`
	hits := parseSuggest(body)
	if len(hits) != 2 {
		t.Fatalf("got %d hits", len(hits))
	}
	if hits[0].Code != "sh508000" || hits[0].Kind != KindCN {
		t.Errorf("沪市 REIT = %+v", hits[0])
	}
	if hits[1].Code != "sz180101" {
		t.Errorf("深市 REIT = %+v", hits[1])
	}
}
