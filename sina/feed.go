package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	restURL  = "https://hq.sinajs.cn/rn=%d&list=%s"
	wsURL    = "wss://w.sinajs.cn/wskt?list=%s"
	referer  = "https://finance.sina.com.cn"
	origin   = "https://gu.sina.cn"
	ua       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
	restStep = 50 // 一次 REST 拉取的最大代码数，避免 URL 过长
)

// decodeGBK 把新浪的 GBK 响应转成 UTF-8。两个接口都是 GBK，无一例外。
func decodeGBK(r io.Reader) (string, error) {
	b, err := io.ReadAll(transform.NewReader(r, simplifiedchinese.GBK.NewDecoder()))
	return string(b), err
}

// Snapshot 拉一次全量快照。WSS 只推变化，休市或无成交的品种一条都不会给，
// 所以开屏和每次改自选后都必须走一遍这里，否则表格是空的。
func Snapshot(ctx context.Context, codes []string) ([]Quote, error) {
	var out []Quote
	for i := 0; i < len(codes); i += restStep {
		batch := codes[i:min(i+restStep, len(codes))]
		u := fmt.Sprintf(restURL, time.Now().UnixMilli(), strings.Join(batch, ","))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Referer", referer) // 缺了这个 REST 直接返回 403
		req.Header.Set("User-Agent", ua)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := decodeGBK(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("快照 HTTP %d", resp.StatusCode)
		}
		out = append(out, ParseFrame(body)...)
	}
	return out, nil
}

// Feed 维护一条到新浪的长连接，并在自选变化时重连。
// 新浪不支持连接内变更订阅，改自选只能带新的 list= 重连。
type Feed struct {
	Out    chan []Quote // 行情批次
	Status chan string  // 连接状态，供 UI 展示

	mu      sync.Mutex
	codes   []string
	full    bool // 下一轮强制全量快照（手动刷新按的 r）
	restart chan struct{}
}

func NewFeed() *Feed {
	return &Feed{
		Out:     make(chan []Quote, 64),
		Status:  make(chan string, 16),
		restart: make(chan struct{}, 1),
	}
}

// Set 替换订阅列表并触发重连。列表没变的（手动刷新）视为强制全量重拉，
// 否则增量快照会算出「没有新代码」而什么都不拉，r 键就成了空操作。
func (f *Feed) Set(codes []string) {
	f.mu.Lock()
	f.full = f.full || slices.Equal(f.codes, codes)
	f.codes = append([]string(nil), codes...)
	f.mu.Unlock()
	select {
	case f.restart <- struct{}{}:
	default: // 已有待处理的重连信号，合并掉
	}
}

func (f *Feed) current() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.codes...)
}

// takeFull 取走并清掉「强制全量」标记。
func (f *Feed) takeFull() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.full
	f.full = false
	return v
}

// missing 挑出还没有快照的代码。加一只自选时只拉这一只，
// 而不是把整个自选表重新拉一遍——盘中连加几只就是连着几次全量请求。
func missing(codes []string, have map[string]bool) []string {
	var out []string
	for _, c := range codes {
		if !have[c] {
			out = append(out, c)
		}
	}
	return out
}

// forceFull 标记下一轮走全量快照。
func (f *Feed) forceFull() {
	f.mu.Lock()
	f.full = true
	f.mu.Unlock()
}

func (f *Feed) say(s string) {
	select {
	case f.Status <- s:
	default:
	}
}

func (f *Feed) emit(qs []Quote) {
	if len(qs) == 0 {
		return
	}
	select {
	case f.Out <- qs:
	default: // UI 跟不上就丢这一批，下一帧会带上最新价
	}
}

// Run 阻塞运行直到 ctx 取消。断线按指数退避重连。
func (f *Feed) Run(ctx context.Context) {
	backoff := 500 * time.Millisecond // 第一次重连等 1s（见 backoffAfter）
	fresh := map[string]bool{}        // 已有当前快照的代码；断线后清空强制全量重拉
	for ctx.Err() == nil {
		// 先吞掉可能已经排队的重连信号，再读列表：Set 若赶在 Run 起步前调用
		// （开屏必然如此），这个信号会让刚连上的连接立刻被自己踢掉，白重连一次。
		// 顺序不能反——先读列表后吞信号会把「读完之后才发生的变更」一起吞了。
		select {
		case <-f.restart:
		default:
		}
		codes := f.current()
		if len(codes) == 0 {
			// 自选为空，等有内容再连。
			select {
			case <-ctx.Done():
			case <-f.restart:
			}
			continue
		}
		if f.takeFull() {
			fresh = map[string]bool{}
		}
		need := missing(codes, fresh)

		// 快照和握手并发跑：串行时开屏要先等 REST（冷启动能到 2s）再等 TLS
		// 握手，白屏四五秒。快照自己会把行情 emit 出去，不必等它。
		go func() {
			if len(need) == 0 {
				return
			}
			t0 := time.Now()
			qs, err := Snapshot(ctx, need)
			if err != nil {
				slog.Error("snapshot", "codes", len(need), "err", err.Error())
				f.say("快照失败: " + err.Error())
				f.forceFull() // 这批没拿到，下一轮重新全量拉，别把它们当已有快照
				return
			}
			slog.Info("snapshot", "codes", len(need), "quotes", len(qs),
				"ms", time.Since(t0).Milliseconds())
			f.emit(qs)
		}()
		for _, c := range need {
			fresh[c] = true
		}

		start := time.Now()
		err := f.stream(ctx, codes)
		lived := time.Since(start)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, errRestart):
			// 主动重连，不算故障——统计断线率时要跟 disconnect 分开看。
			slog.Info("resubscribe", "lived_s", lived.Seconds(), "codes", len(codes))
			backoff = 500 * time.Millisecond
			continue
		}
		// 断线期间漏掉的推送只能靠全量快照补回来。
		fresh = map[string]bool{}
		backoff = backoffAfter(lived, backoff)
		slog.Warn("disconnect", "err", err.Error(), "lived_s", lived.Seconds(),
			"backoff_s", backoff.Seconds(), "codes", len(codes))
		f.say(fmt.Sprintf("断线: %v，%s 后重连", err, backoff))
		select {
		case <-ctx.Done():
			return
		case <-f.restart:
			backoff = 500 * time.Millisecond
		case <-time.After(backoff):
		}
	}
}

// backoffAfter 给出这次断线该等多久：连上撑过一分钟的，算服务端正常掐线而不是
// 故障，退避从头来过。新浪每隔几分钟就 EOF 一次，一路翻倍的话很快就卡死在
// 30 秒——盘中每次断线都要黑屏半分钟。
func backoffAfter(lived, prev time.Duration) time.Duration {
	if lived > time.Minute {
		return time.Second
	}
	return min(prev*2, 30*time.Second)
}

var errRestart = errors.New("订阅变更")

// stream 连一次 WSS 并读到断开。自选变化时返回 errRestart。
func (f *Feed) stream(ctx context.Context, codes []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 握手头（Upgrade/Connection/Sec-WebSocket-*）由库按 RFC 6455 自动生成，
	// 手抄浏览器那份反而会握手失败。这里只带业务需要的 Origin。
	// 不能对 list 做 URL 编码：新浪要的是原样的逗号和 `gb_$ixic` 里的 `$`，
	// 编码成 %2C/%24 后服务端会直接回 close 帧。
	u := fmt.Sprintf(wsURL, strings.Join(codes, ","))
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {origin}, "User-Agent": {ua}},
	})
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	slog.Info("connect", "codes", len(codes))
	var frames, quotes int
	defer func() { slog.Info("stream_end", "frames", frames, "quotes", quotes) }()
	f.say(fmt.Sprintf("已连接 · %d 个品种", len(codes)))

	// 自选变化时中断读取，让 Run 带新列表重连。
	restarted := make(chan struct{})
	go func() {
		select {
		case <-f.restart:
			close(restarted)
			cancel()
		case <-ctx.Done():
		}
	}()

	// 休市或冷门品种可能几分钟没有一帧，中间的 NAT/代理会把静默连接掐掉。
	// 定期发 ping 保活；失败不用管，紧接着的 Read 会带着真正的错误返回。
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
				_ = c.Ping(pctx)
				pcancel()
			}
		}
	}()

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			select {
			case <-restarted:
				return errRestart
			default:
			}
			return err
		}
		// WSS 帧本身就是 UTF-8，只有 REST 快照是 GBK。两个接口编码不一致，
		// 这里再解一次 GBK 会把中文名变成乱码。
		qs := ParseFrame(string(data))
		frames, quotes = frames+1, quotes+len(qs)
		f.emit(qs)
	}
}
