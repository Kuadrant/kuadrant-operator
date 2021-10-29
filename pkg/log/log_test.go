// +build unit
/*
Copyright 2021 Red Hat, Inc.

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

package log

import (
	"testing"

	// In this package there is no ginkgo tests
	// Required to parse command line ginkgo flags -ginkgo.v -ginkgo.progress
	_ "github.com/onsi/ginkgo"
	"gotest.tools/assert"
)

func TestToLevel(t *testing.T) {
	assert.Equal(t, int(ToLevel("debug")), -1)
	assert.Equal(t, int(ToLevel("info")), 0)
	assert.Equal(t, int(ToLevel("warn")), 1)
	assert.Equal(t, int(ToLevel("error")), 2)
	assert.Equal(t, int(ToLevel("dpanic")), 3)
	assert.Equal(t, int(ToLevel("panic")), 4)
	assert.Equal(t, int(ToLevel("fatal")), 5)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("ToLevel(\"invalid\") should have panicked!")
			}
		}()
		// should panic
		ToLevel("invalid")
	}()
}

func TestToMode(t *testing.T) {
	assert.Equal(t, int(ToMode("production")), 0)
	assert.Equal(t, int(ToMode("development")), 1)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("ToMode(\"invalid\") should have panicked!")
			}
		}()
		// should panic
		ToMode("invalid")
	}()
}
