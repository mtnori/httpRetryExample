package http

import (
	retryabletransport "httpRetry/internal/pkg/http/transport"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"time"
)

func NewClient() *http.Client {
	transport := retryabletransport.NewRetryableTransport(
		http.DefaultTransport,
		3,
		shouldRetry,
		exponentialBackoffAndFullJitter(1000, 10000),
	)

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}

//func backoff(attempts int) time.Duration {
//	return time.Duration(math.Pow(2, float64(attempts))) * time.Second
//}

func exponentialBackoffAndFullJitter(baseMills int, capMills int) retryabletransport.BackoffFunc {
	return func(attempts int) time.Duration {
		tempWaitMills := baseMills * int(math.Pow(2, float64(attempts)))
		if tempWaitMills > capMills {
			tempWaitMills = capMills
		}
		slog.Info("tempWaitMills", "wait", tempWaitMills)

		waitMills := rand.Intn(tempWaitMills)
		slog.Info("waitMills", "wait", waitMills)
		return time.Duration(waitMills) * time.Millisecond
	}
}

func shouldRetry(res *http.Response, err error) bool {
	if err != nil {
		return true
	}

	if res.StatusCode >= http.StatusInternalServerError {
		return true
	}
	return false
}
