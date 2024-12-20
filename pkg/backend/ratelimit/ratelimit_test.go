// Lophiid distributed honeypot
// Copyright (C) 2024 Niels Heinen
//
// This program is free software; you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the
// Free Software Foundation; either version 2 of the License, or (at your
// option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY
// or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License
// for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, write to the Free Software Foundation, Inc.,
// 59 Temple Place, Suite 330, Boston, MA 02111-1307 USA
package ratelimit

import (
	"fmt"
	"lophiid/pkg/database/models"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRateLimitOk(t *testing.T) {
	testRateWindow := time.Second * 5
	testBucketDuration := time.Second
	testMaxIpRequestsPerWindow := 4
	testMaxIpRequestPerBucket := 2

	testMaxUriRequestsPerWindow := 6
	testMaxUriRequestPerBucket := 6

	req := models.Request{
		HoneypotIP: "1.1.1.1",
		SourceIP:   "2.2.2.2",
		Port:       31337,
		Uri:        "/aa",
	}
	reg := prometheus.NewRegistry()
	rMetrics := CreateRatelimiterMetrics(reg)
	r := NewWindowRateLimiter(testRateWindow, testBucketDuration, testMaxIpRequestsPerWindow, testMaxIpRequestPerBucket, testMaxUriRequestsPerWindow, testMaxUriRequestPerBucket, rMetrics)

	if testutil.ToFloat64(rMetrics.ipRateBucketsGauge) != 0 {
		t.Errorf("rateBucketsGauge should be 0 at the start")
	}

	// Simulate multiple requests in the same bucket. It should
	// work OK twice and be rejected a third time due to the
	// MaxRequestPerBucket being set to 2.
	if isAllowed, err := r.AllowRequest(&req); !isAllowed {
		t.Errorf("not allowed, unexpected error %v", err)
	}
	if isAllowed, err := r.AllowRequest(&req); !isAllowed {
		t.Errorf("not allowed, unexpected error %v", err)
	}

	// This is the third one and needs to be rejected.
	isAllowed, err := r.AllowRequest(&req)
	if isAllowed {
		t.Errorf("request is allowed but it should be rejected")
	}

	if err != ErrIPBucketLimitExceeded {
		t.Errorf("expected bucket exceeded, got unexpected error %v", err)
	}

	// Now we do a tick twice which resets the bucket limit. Therefore
	// the next request is allowed again.
	r.Tick()
	if isAllowed, err = r.AllowRequest(&req); !isAllowed {
		t.Errorf("unexpected error %v", err)
	}

	// Now the window limit is going to exceed though.
	isAllowed, err = r.AllowRequest(&req)
	if isAllowed {
		t.Errorf("request exceeds window limit and should be rejected")
	}

	if err != ErrIPWindowLimitExceeded {
		t.Errorf("expected ErrWindowLimitExceeded but got %v", err)
	}

	m := testutil.ToFloat64(rMetrics.ipRateBucketsGauge)
	if m != 1 {
		t.Errorf("rateBucketsGauge should be 1, is %f", m)
	}

	// Now continue ticking until the window is empty and removed.
	r.Tick()
	r.Tick()
	r.Tick()
	r.Tick()
	r.Tick()

	// Check if the RateBucket entry is indeed removed.
	m = testutil.ToFloat64(rMetrics.ipRateBucketsGauge)
	if m != 0 {
		t.Errorf("rateBucketsGauge should be 0 after reset")
	}

	if isAllowed, err := r.AllowRequest(&req); !isAllowed {
		t.Errorf("unexpected error %v", err)
	}
}

func TestAllowRequestForIP(t *testing.T) {
	tests := []struct {
		name          string
		req           *models.Request
		requestCount  int
		expectedAllow bool
		expectedError error
		setupRequests int  // number of requests to make before the actual test
		newBucket     bool // whether this should create a new bucket
	}{
		{
			name: "first request for IP creates bucket",
			req: &models.Request{
				HoneypotIP: "10.0.0.1",
				SourceIP:   "192.168.1.1",
				Port:       8080,
			},
			requestCount:  1,
			expectedAllow: true,
			expectedError: nil,
			newBucket:     true,
		},
		{
			name: "request within limits",
			req: &models.Request{
				HoneypotIP: "10.0.0.1",
				SourceIP:   "192.168.1.2",
				Port:       8080,
			},
			requestCount:  2,
			expectedAllow: true,
			expectedError: nil,
		},
		{
			name: "bucket limit exceeded",
			req: &models.Request{
				HoneypotIP: "10.0.0.1",
				SourceIP:   "192.168.1.3",
				Port:       8080,
			},
			requestCount:  3,
			setupRequests: 2, // make 2 requests first to reach the limit
			expectedAllow: false,
			expectedError: ErrIPBucketLimitExceeded,
		},
		{
			name: "window limit exceeded",
			req: &models.Request{
				HoneypotIP: "10.0.0.1",
				SourceIP:   "192.168.1.4",
				Port:       8080,
			},
			requestCount:  5,
			setupRequests: 4, // make 4 requests first to reach the window limit
			expectedAllow: false,
			expectedError: ErrIPWindowLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new rate limiter for each test
			r := NewWindowRateLimiter(
				time.Second*5, // window
				time.Second,   // bucket duration
				4,             // max requests per window
				2,             // max requests per bucket
				10,            // max URI requests per window (not used in this test)
				5,             // max URI requests per bucket (not used in this test)
				CreateRatelimiterMetrics(prometheus.NewRegistry()),
			)

			// Perform setup requests if needed
			for i := 0; i < tt.setupRequests; i++ {
				r.allowRequestForIP(tt.req)
			}

			// Record initial bucket state if we're testing new bucket creation
			if tt.newBucket {
				ipRateKey := fmt.Sprintf("%s-%d-%s", tt.req.HoneypotIP, tt.req.Port, tt.req.SourceIP)
				if _, exists := r.IPRateBuckets[ipRateKey]; exists {
					t.Errorf("bucket should not exist before first request")
				}
			}

			// Perform the test request
			allowed, err := r.allowRequestForIP(tt.req)

			// Verify results
			if allowed != tt.expectedAllow {
				t.Errorf("allowRequestForIP() allowed = %v, want %v", allowed, tt.expectedAllow)
			}

			if err != tt.expectedError {
				t.Errorf("allowRequestForIP() error = %v, want %v", err, tt.expectedError)
			}

			// Verify bucket creation if applicable
			if tt.newBucket {
				ipRateKey := fmt.Sprintf("%s-%d-%s", tt.req.HoneypotIP, tt.req.Port, tt.req.SourceIP)
				if _, exists := r.IPRateBuckets[ipRateKey]; !exists {
					t.Errorf("bucket should exist after first request")
				}
			}
		})
	}
}

func TestAllowRequestForURI(t *testing.T) {
	tests := []struct {
		name           string
		req            *models.Request
		requestCount   int
		expectedAllow  bool
		expectedError  error
		setupRequests  int  // number of requests to make before the actual test
		newBucket     bool // whether this should create a new bucket
	}{
		{
			name: "first request for URI creates bucket",
			req: &models.Request{
				BaseHash: "hash1",
			},
			requestCount:  1,
			expectedAllow: true,
			expectedError: nil,
			newBucket:    true,
		},
		{
			name: "request within limits",
			req: &models.Request{
				BaseHash: "hash2",
			},
			requestCount:  2,
			expectedAllow: true,
			expectedError: nil,
		},
		{
			name: "bucket limit exceeded",
			req: &models.Request{
				BaseHash: "hash3",
			},
			requestCount:  3,
			setupRequests: 5, // make 5 requests first to reach the bucket limit
			expectedAllow: false,
			expectedError: ErrURIBucketLimitExceeded,
		},
		{
			name: "window limit exceeded",
			req: &models.Request{
				BaseHash: "hash4",
			},
			requestCount:  7,
			setupRequests: 6, // make 6 requests first to reach the window limit
			expectedAllow: false,
			expectedError: ErrURIWindowLimitExceeded,
		},
		{
			name: "different URIs don't interfere",
			req: &models.Request{
				BaseHash: "hash5",
			},
			requestCount:  1,
			expectedAllow: true,
			expectedError: nil,
			newBucket:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new rate limiter for each test
			r := NewWindowRateLimiter(
				time.Second*5,        // window
				time.Second,          // bucket duration
				10,                   // max IP requests per window (not used in this test)
				5,                    // max IP requests per bucket (not used in this test)
				6,                    // max URI requests per window
				5,                    // max URI requests per bucket
				CreateRatelimiterMetrics(prometheus.NewRegistry()),
			)

			// Perform setup requests if needed
			for i := 0; i < tt.setupRequests; i++ {
				if tt.name == "window limit exceeded" {
					// For window limit test, directly set up the buckets
					if i == 0 {
						r.URIRateBuckets[tt.req.BaseHash] = make([]int, r.NumberBuckets)
					}
					// Spread requests across buckets to reach window limit
					r.URIRateBuckets[tt.req.BaseHash][i%r.NumberBuckets] = 2
				} else {
					r.allowRequestForURI(tt.req)
				}
			}

			// Record initial bucket state if we're testing new bucket creation
			if tt.newBucket {
				uriRateKey := tt.req.BaseHash
				if _, exists := r.URIRateBuckets[uriRateKey]; exists {
					t.Errorf("bucket should not exist before first request")
				}
			}

			// Perform the test request
			allowed, err := r.allowRequestForURI(tt.req)

			// Verify results
			if allowed != tt.expectedAllow {
				t.Errorf("allowRequestForURI() allowed = %v, want %v", allowed, tt.expectedAllow)
			}

			if err != tt.expectedError {
				t.Errorf("allowRequestForURI() error = %v, want %v", err, tt.expectedError)
			}

			// Verify bucket creation if applicable
			if tt.newBucket {
				uriRateKey := tt.req.BaseHash
				if _, exists := r.URIRateBuckets[uriRateKey]; !exists {
					t.Errorf("bucket should exist after first request")
				}
			}
		})
	}
}
