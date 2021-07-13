// Copyright 2021 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package healthcheck tests and communicates the health of the Cloud SQL Auth proxy.
package healthcheck

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/GoogleCloudPlatform/cloudsql-proxy/logging"
	"github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/proxy"
)

const (
	livenessPath = "/liveness"
	readinessPath = "/readiness"
)

// HC is a type used to implement health checks for the proxy.
type HC struct {
	// startedL protects started, a flag that indicates whether the proxy is 
	// done starting up. started is used to support readiness probing and 
	// should not be confused for affecting startup probing.
	startedL sync.Mutex
	started bool
	// port designates the port number on which HC listens and serves.
	port string
	// srv is a pointer to the HTTP server used to communicated proxy health.
	srv *http.Server
}

// NewHealthCheck initializes a HC object and exposes HTTP endpoints used to
// communicate proxy health.
func NewHealthCheck(c *proxy.Client, port string) (*HC, error) {
	mux := http.NewServeMux()

	srv := &http.Server{
		Addr: ":" + port,
		Handler: mux,
	}

	hc := &HC{
		port: port,
		srv:  srv,
	}

	mux.HandleFunc(readinessPath, func(w http.ResponseWriter, _ *http.Request) {
		if !isReady(c, hc) {
			w.WriteHeader(500)
			w.Write([]byte("error"))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc(livenessPath, func(w http.ResponseWriter, _ *http.Request) {
		if !isLive() { // Because isLive() returns true, this case should not be reached.
			w.WriteHeader(500)
			w.Write([]byte("error"))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logging.Errorf("Failed to serve: %v", err)
		}
	}()

	return hc, nil
}

// Close gracefully shuts down the HTTP server belonging to the HC object.
func (hc *HC) Close(ctx context.Context) error {
	err := hc.srv.Shutdown(ctx)
	return err
}

// NotifyStarted tells the HC that the proxy has finished startup.
func (hc *HC) NotifyStarted() {
	hc.startedL.Lock()
	hc.started = true
	hc.startedL.Unlock()
}

// isLive returns true as long as the proxy is running.
func isLive() bool {
	return true
}

// isReady will check the following criteria before determining whether the
// proxy is ready for new connections.
// 1. Finished starting up / been sent the 'Ready for Connections' log.
// 2. Not yet hit the MaxConnections limit, if applicable.
func isReady(c *proxy.Client, hc *HC) bool {
	// Not ready until we reach the 'Ready for Connections' log.
	hc.startedL.Lock()
	started := hc.started
	hc.startedL.Unlock()

	if !started {
		logging.Errorf("Readiness failed because proxy has not finished starting up.")
		return false
	}

	// Not ready if the proxy is at the optional MaxConnections limit.
	if c.MaxConnections > 0 && atomic.LoadUint64(&c.ConnectionsCounter) >= c.MaxConnections {
		logging.Errorf("Readiness failed because proxy has reached the maximum connections limit (%d).", c.MaxConnections)
		return false
	}

	return true
}
