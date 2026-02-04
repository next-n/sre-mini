package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var ready int32 = 1

var (
	reqTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "http_requests_total", Help: "Total HTTP requests"},
		[]string{"path", "code"},
	)
	reqLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path"},
	)
	inFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_in_flight_requests",
			Help: "Current number of in-flight HTTP requests.",
		},
	)
)

func burnCPU(d time.Duration) {
	deadline := time.Now().Add(d)
	var x uint64 = 1
	for time.Now().Before(deadline) {
		// cheap integer ops to keep CPU busy
		x = x*1664525 + 1013904223
		if x%7 == 0 {
			x ^= x << 13
		}
	}
}

func main() {
	prometheus.MustRegister(reqTotal, reqLatency, inFlight)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&ready) == 1 {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not-ready"))
	})

	mux.HandleFunc("/fail/ready", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&ready, 0)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("readiness=false"))
	})

	mux.HandleFunc("/recover/ready", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&ready, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("readiness=true"))
	})

	mux.HandleFunc("/panic", instrument("/panic", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "intentional request failure", http.StatusInternalServerError)
	}))
	mux.HandleFunc("/crash", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("crashing process\n"))
		w.(http.Flusher).Flush()
		os.Exit(1)
	})

	// Baseline: no intentional problems
	mux.HandleFunc("/work", instrument("/work", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"ok":   true,
			"mode": "fine",
			"time": time.Now().UTC().Format(time.RFC3339Nano),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))

	// 1) CPU increase: triggers HPA via CPU utilization
	mux.HandleFunc("/work/cpu", instrument("/work/cpu", func(w http.ResponseWriter, r *http.Request) {
		// CPU burn: adjust to trigger HPA
		burnCPU(1200 * time.Millisecond)

		resp := map[string]any{
			"ok":   true,
			"mode": "cpu",
			"burn": "1200ms",
			"time": time.Now().UTC().Format(time.RFC3339Nano),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))

	// 2) Latency: triggers latency alert, not CPU/HPA (unless your handler does CPU too)
	mux.HandleFunc("/work/latency", instrument("/work/latency", func(w http.ResponseWriter, r *http.Request) {
		// simulate steady latency to make alert behavior predictable in drills
		delay := 2 * time.Second
		time.Sleep(delay)

		resp := map[string]any{
			"ok":    true,
			"mode":  "latency",
			"delay": delay.String(),
			"time":  time.Now().UTC().Format(time.RFC3339Nano),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))

	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// graceful shutdown (no manual intervention during rollout)
	go func() {
		log.Printf("listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 2)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Printf("shutdown started")
	atomic.StoreInt32(&ready, 0) // stop receiving traffic before exiting

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Printf("shutdown complete")
}

func instrument(path string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &respWriter{ResponseWriter: w, code: 200}

		h(rw, r)

		dur := time.Since(start).Seconds()
		reqLatency.WithLabelValues(path).Observe(dur)
		reqTotal.WithLabelValues(path, fmt.Sprintf("%d", rw.code)).Inc()
	}
}

type respWriter struct {
	http.ResponseWriter
	code int
}

func (r *respWriter) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (r *respWriter) WriteHeader(statusCode int) {
	r.code = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlight.Inc()
		defer inFlight.Dec()

		start := time.Now()
		rw := &respWriter{ResponseWriter: w, code: 200}

		next.ServeHTTP(rw, r)

		log.Printf(`{"path":%q,"method":%q,"code":%d,"ms":%d}`,
			r.URL.Path, r.Method, rw.code, time.Since(start).Milliseconds())
	})
}
