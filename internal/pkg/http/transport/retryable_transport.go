package transport

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// CheckRetryFunc は、レスポンスとエラー内容から、リトライを行うか判定する関数の型定義
type CheckRetryFunc func(*http.Response, error) bool

// BackoffFunc は、バックオフを取得する関数の型定義
type BackoffFunc func(attempts int) time.Duration

// RetryableTransport はリトライを行うための http.RoundTripper 具象型
type RetryableTransport struct {
	wrapped     http.RoundTripper
	maxAttempts int
	checkRetry  CheckRetryFunc
	backoff     BackoffFunc
}

// NewRetryableTransport は RetryableTransport 構造体を作成する
func NewRetryableTransport(transport http.RoundTripper, maxRetryCounts int,
	shouldRetry CheckRetryFunc, backoff BackoffFunc) *RetryableTransport {
	return &RetryableTransport{
		wrapped:     transport,
		maxAttempts: maxRetryCounts,
		checkRetry:  shouldRetry,
		backoff:     backoff,
	}
}

// drainBody はレスポンスボディを読み切る
// NOTE: コネクションを再利用するには、レスポンスボディを読み切ってクローズする必要がある
func drainBody(res *http.Response) error {
	if res.Body != nil {
		_, err := io.Copy(io.Discard, res.Body)
		if err != nil {
			return err
		}
		err = res.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// readTrackingBody は io.ReadCloser の具象型。http.Request の Body をラップするために使用する
// readTrackingBody.Read と readTrackingBody.Close メソッドを実装することで io.ReadCloser インターフェースを満たす
type readTrackingBody struct {
	io.ReadCloser
	didRead  bool
	didClose bool
}

func (r *readTrackingBody) Read(data []byte) (int, error) {
	r.didRead = true
	return r.ReadCloser.Read(data)
}

func (r *readTrackingBody) Close() error {
	r.didClose = true
	return r.ReadCloser.Close()
}

func setupRewindBody(req *http.Request) *http.Request {
	if req.Body == nil || req.Body == http.NoBody {
		return req
	}
	newReq := *req
	newReq.Body = &readTrackingBody{ReadCloser: req.Body}
	return &newReq
}

// rewindBody はリクエストボディを巻き戻す
// NOTE: bytes.Buffer など一部の io.ReadCloser 具象型では、リトライ時に冪等なリクエストにならないため巻き戻す必要がある
func rewindBody(req *http.Request) (rewoundBody *http.Request, err error) {
	// リクエストボディがない、または読み込み、クローズが行われている場合は巻き戻さない
	if req.Body == nil || req.Body == http.NoBody || (!req.Body.(*readTrackingBody).didRead && !req.Body.(*readTrackingBody).didClose) {
		return req, nil
	}

	// リクエストボディがクローズされていない場合はクローズする
	if !req.Body.(*readTrackingBody).didClose {
		err := req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	var body io.ReadCloser

	if req.GetBody != nil {
		body, err = req.GetBody()
		if err != nil {
			return nil, err
		}
	}

	if req.GetBody == nil {
		buf, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = io.NopCloser(bytes.NewReader(buf))
	}

	newReq := *req
	newReq.Body = &readTrackingBody{
		ReadCloser: body,
	}
	return &newReq, nil
}

// transport は親の Transport を返却する。親がない場合は、http.DefaultTransport を返却する
func (t *RetryableTransport) transport() http.RoundTripper {
	if t.wrapped == nil {
		return http.DefaultTransport
	}
	return t.wrapped
}

// RoundTrip はリクエスト送信エラーの場合にリトライを行う
// NOTE: このメソッドを実装することで、transport.RetryableTransport は http.RoundTripper インターフェースを満たす
func (t *RetryableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// コンテキストを取得する
	ctx := req.Context()

	// 巻き戻せるように、状態を持った構造体にラップする
	req = setupRewindBody(req)

	// リトライ処理
	var attempts int
	for {
		attempts++

		// 巻き戻したリクエストボディを取得する
		rewoundReq, err := rewindBody(req)

		slog.Debug("request start")

		// リクエストを送信
		res, err := t.transport().RoundTrip(rewoundReq)

		slog.Debug("request end")

		// リトライ不要なら結果を返却する
		shouldRetry := t.checkRetry(res, err)
		if !shouldRetry {
			return res, err
		}

		// 試行回数が上限なら結果を返却する
		if t.maxAttempts < attempts {
			return res, err
		}

		// リトライまでのバックオフを取得する
		wait := t.backoff(attempts)

		slog.Info("backoff", "wait", wait)

		// 呼び出し元でタイムアウトやキャンセルされている場合があるので、処理を継続する必要があるか確認する
		// NOTE: Transport に CancelRequest を実装する方法もあるが、CancelRequest は HTTP/2 をキャンセルできないので非推奨
		select {
		// context.Context が終了していれば、エラーを返却する
		case <-ctx.Done():
			return nil, ctx.Err()
		// 遅延処理を行う
		case <-time.After(wait):
		}

		// コネクションを再利用するためにレスポンスボディを読み切ってクローズする
		err = drainBody(res)
		if err != nil {
			return nil, err
		}
	}
}
