// Copyright 2015 Google Inc. All Rights Reserved.
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
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/GoogleCloudPlatform/cloudsql-proxy/logging"
	"github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/proxy"
)

// HealthCheck is a type used to implement health checks for the proxy.
type HealthCheck struct {
	// live and ready correspond to liveness and readiness probing in Kubernetes
	// health checks
	live  bool
	ready bool
	// started is used to support readiness probing and should not be confused
	// for relating to startup probing.
	started bool
}

func InitHealthCheck(proxyClient *proxy.Client) *HealthCheck {
	fmt.Printf("initializing healthcheck\n")
	hc := &HealthCheck{
		live:    true,
		ready:   false,
		started: false,
	}

	// Handlers used to set up HTTP endpoint for communicating proxy health.
	http.HandleFunc("/readiness", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Printf("hit readiness endpoint\n")
		hc.ready = readinessTest(proxyClient, hc)
		if hc.ready {
			w.WriteHeader(200)
			w.Write([]byte("ok\n"))
		} else {
			w.WriteHeader(500)
			w.Write([]byte("error\n"))
		}
	})

	http.HandleFunc("/liveness", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Printf("hit liveness endpoint\n")
		hc.live = livenessTest()
		if hc.live {
			w.WriteHeader(200)
			w.Write([]byte("ok\n"))
		} else {
			w.WriteHeader(500)
			w.Write([]byte("error\n"))
		}
	})

	fmt.Printf("endpoints created\n")

	go func() {
		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			logging.Errorf("Failed to start endpoint(s).")
		}
	}()

	return hc
}

func NotifyReady(hc *HealthCheck) {
	fmt.Printf("ready notification received\n")
	hc.started = true
}

// livenessTest returns true as long as the proxy is running.
func livenessTest() bool {
	fmt.Printf("running liveness test\n")
	return true
}

// readinessTest checks several criteria before determining the proxy is ready.
func readinessTest(proxyClient *proxy.Client, hc *HealthCheck) bool {
	fmt.Printf("running readiness test\n")

	// Wait until the 'Ready For Connections' log to mark the proxy client as ready.
	if !hc.started {
		fmt.Printf("readiness test failed because startup was not completed\n")
		return false
	}

	// Mark not ready if the proxy client is at MaxConnections
	// (Parts of this code are taken from client.go)
	active := atomic.AddUint64(&proxyClient.ConnectionsCounter, 1)

	// Defer decrementing ConnectionsCounter upon connections closing.
	defer atomic.AddUint64(&proxyClient.ConnectionsCounter, ^uint64(0))

	if proxyClient.MaxConnections > 0 && active > proxyClient.MaxConnections {
		return false
	}

	return true
}
