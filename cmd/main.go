package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	myhttp "httpRetry/internal/pkg/http"
	"io"
	"log"
	"log/slog"
	"net/http"
)

type RequestBody struct {
	Name string `json:"name"`
}

func main() {
	var debugLevel = new(slog.LevelVar)
	debugLevel.Set(slog.LevelDebug)

	client := myhttp.NewClient()

	body := RequestBody{
		Name: "Nori",
	}
	bodyJson, err := json.Marshal(body)
	if err != nil {
		log.Fatal(err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, "https://httpbin.org/status/200:0.2,500:0.8", bytes.NewReader(bodyJson))
	req.Header.Set("Content-Type", "application/json")
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	defer res.Body.Close()
	fmt.Println(res.Status)

	b, err := io.ReadAll(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s", b)
}
