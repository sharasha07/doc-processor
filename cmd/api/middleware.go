package main

import (
	"expvar"
	"fmt"
	"net/http"
	"strconv"

	"github.com/felixge/httpsnoop"
)

var (
	totalRequestsReceived      = expvar.NewInt("total_requests_received")
	totalResponsesSent         = expvar.NewInt("total_responses_sent")
	totalProcessingTimeSeconds = expvar.NewFloat("total_processing_time_seconds")
	totalResponsesSentByStatus = expvar.NewMap("total_responses_sent_by_status")
)

var (
	geminiInputTokens           = expvar.NewInt("gemini_input_tokens")
	geminiOutputTokens          = expvar.NewInt("gemini_output_tokens")
	geminiTotalTokens           = expvar.NewInt("gemini_total_tokens")
	geminiEstimatedCostUSD      = expvar.NewFloat("gemini_estimated_cost_usd")
	geminiProcessingTimeSeconds = expvar.NewFloat("gemini_processing_time_seconds")
	geminiTaskStatus            = expvar.NewMap("gemini_task_status")
)

func (app *application) metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalRequestsReceived.Add(1)

		metrics := httpsnoop.CaptureMetrics(next, w, r)

		totalResponsesSent.Add(1)
		totalProcessingTimeSeconds.Add(metrics.Duration.Seconds())
		totalResponsesSentByStatus.Add(strconv.Itoa(metrics.Code), 1)
	})
}

func (app *application) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.Header().Set("Connection", "close")
				app.serverErrorResponse(w, r, fmt.Errorf("%v", err))
			}
		}()

		next.ServeHTTP(w, r)
	})
}
