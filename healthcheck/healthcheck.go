// Copyright 2021 Google Inc. All Rights Reserved.
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
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/GoogleCloudPlatform/cloudsql-proxy/logging"
	"github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/proxy"
)

var (
	readinessMutex = &sync.Mutex{}
	livenessMutex = &sync.Mutex{}
	startupMutex = &sync.Mutex{}
)

// HealthCheck is a type used to implement health checks for the proxy.
type HC struct {
	// live and ready correspond to liveness and readiness probing in Kubernetes
	// health checks
	live  bool
	ready bool
	// started is used to support readiness probing and should not be confused
	// for relating to startup probing.
	started bool
	// srv is a pointer to the HTTP server
	srv *http.Server
}

// NewHealthCheck initializes a HC object and exposes the appropriate HTTP endpoints
// for communicating proxy health.
func NewHealthCheck(proxyClient *proxy.Client) *HC {
	srv := &http.Server{
		Addr: ":8080", // TODO: Pick a good port.
	}

	hc := &HC{
		live: true,
		srv:  srv,
	}

	// Handlers used to set up HTTP endpoint for communicating proxy health.
	http.HandleFunc("/readiness", func(w http.ResponseWriter, _ *http.Request) {
		readinessMutex.Lock()
		hc.ready = readinessTest(proxyClient, hc)
		if !hc.ready {
			readinessMutex.Unlock()
			w.WriteHeader(500)
			w.Write([]byte("error\n"))
			return
		}
		readinessMutex.Unlock()

		w.WriteHeader(200)
		w.Write([]byte("ok\n"))
	})

	http.HandleFunc("/liveness", func(w http.ResponseWriter, _ *http.Request) {
		livenessMutex.Lock()
		hc.live = livenessTest()
		if !hc.live {
			livenessMutex.Unlock()
			w.WriteHeader(500)
			w.Write([]byte("error\n"))
			return
		}
		livenessMutex.Unlock()
		
		w.WriteHeader(200)
		w.Write([]byte("ok\n"))
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Errorf("Failed to start endpoint(s): %v", err)
		}
	}()

	return hc
}

// NotifyReadyForConnections changes the value of 'started' in a health
// check object to true, marking the proxy as done starting up.
func (hc *HC) NotifyReadyForConnections() {
	if hc != nil {
		startupMutex.Lock()
		hc.started = true
		startupMutex.Unlock()
	}
}

// livenessTest returns true as long as the proxy is running.
func livenessTest() bool {
	return true
}

// readinessTest checks several criteria before determining the proxy is ready.
func readinessTest(proxyClient *proxy.Client, hc *HC) bool {
	// Wait until the 'Ready For Connections' log to mark the proxy client as ready.
	startupMutex.Lock()
	if !hc.started {
		startupMutex.Unlock()
		return false
	}
	startupMutex.Unlock()

	// Mark not ready if the proxy client is at MaxConnections.
	if proxyClient.MaxConnections > 0 && atomic.LoadUint64(&proxyClient.ConnectionsCounter) >= proxyClient.MaxConnections {
		return false
	}

	return true
}
