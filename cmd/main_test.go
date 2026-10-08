/*
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"testing"
	"time"
)

func TestBindManagerFlags(t *testing.T) {
	t.Run("default flags", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		var pollInterval, orphanGracePeriod time.Duration
		bindFlags(fs, &pollInterval, &orphanGracePeriod)

		if err := fs.Parse([]string{}); err != nil {
			t.Fatalf("unexpected error parsing flags: %v", err)
		}
		if pollInterval != 10*time.Second {
			t.Errorf("expected default pollInterval 10s, got %v", pollInterval)
		}
		if orphanGracePeriod != 1*time.Minute {
			t.Errorf("expected default orphanGracePeriod 1m, got %v", orphanGracePeriod)
		}
	})

	t.Run("custom flags", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		var pollInterval, orphanGracePeriod time.Duration
		bindFlags(fs, &pollInterval, &orphanGracePeriod)

		args := []string{"--poll-interval=5s", "--orphan-grace-period=30s"}
		if err := fs.Parse(args); err != nil {
			t.Fatalf("unexpected error parsing flags: %v", err)
		}
		if pollInterval != 5*time.Second {
			t.Errorf("expected custom pollInterval 5s, got %v", pollInterval)
		}
		if orphanGracePeriod != 30*time.Second {
			t.Errorf("expected custom orphanGracePeriod 30s, got %v", orphanGracePeriod)
		}
	})
}
