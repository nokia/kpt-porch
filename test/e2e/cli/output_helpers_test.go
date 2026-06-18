// Copyright 2026 The kpt Authors
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

package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripColumns(t *testing.T) {
	testCases := map[string]struct {
		input   string
		columns []string
		want    string
	}{
		"strips single column": {
			input:   "NAME       REVISION   AGE\nmy-pkg     1          3s\nother-pkg  2          5s\n",
			columns: []string{"AGE"},
			want:    "NAME       REVISION\nmy-pkg     1\nother-pkg  2\n",
		},
		"strips middle column": {
			input:   "NAME       LATEST   LIFECYCLE\nmy-pkg     true     Published\nother-pkg  false    Draft\n",
			columns: []string{"LATEST"},
			want:    "NAME       LIFECYCLE\nmy-pkg     Published\nother-pkg  Draft\n",
		},
		"strips multiple columns": {
			input:   "NAME       REVISION   LATEST   AGE\nmy-pkg     1          true     3s\n",
			columns: []string{"LATEST", "AGE"},
			want:    "NAME       REVISION\nmy-pkg     1\n",
		},
		"no-op when column not found": {
			input:   "NAME       REVISION\nmy-pkg     1\n",
			columns: []string{"NOTHERE"},
			want:    "NAME       REVISION\nmy-pkg     1\n",
		},
		"no-op with empty columns list": {
			input:   "NAME       REVISION\nmy-pkg     1\n",
			columns: nil,
			want:    "NAME       REVISION\nmy-pkg     1\n",
		},
		"handles empty input": {
			input:   "",
			columns: []string{"AGE"},
			want:    "",
		},
		"strips last column with values wider than header": {
			input:   "NAME       REVISION   AGE\nmy-pkg     1          12m34s\nother-pkg  2          2h5m\n",
			columns: []string{"AGE"},
			want:    "NAME       REVISION\nmy-pkg     1\nother-pkg  2\n",
		},
		"does not match column name as substring of another column": {
			input:   "NAME       PACKAGE   WORKSPACENAME   AGE\nmy-pkg     basens    v1              3s\n",
			columns: []string{"AGE"},
			want:    "NAME       PACKAGE   WORKSPACENAME\nmy-pkg     basens    v1\n",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := stripColumns(tc.input, tc.columns)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestReplacePlaceholders(t *testing.T) {
	testCases := map[string]struct {
		expected string
		input    string
		want     string
	}{
		"replaces ANY with actual value": {
			expected: "my-pkg 1 true Published {{ANY}}\n",
			input:    "my-pkg 1 true Published 3s\n",
			want:     "my-pkg 1 true Published 3s\n",
		},
		"multiple placeholders on one line": {
			expected: "my-pkg {{ANY}} true {{ANY}}\n",
			input:    "my-pkg 1 true 3s\n",
			want:     "my-pkg 1 true 3s\n",
		},
		"no placeholders returns expected unchanged": {
			expected: "my-pkg 1 true Published\n",
			input:    "my-pkg 1 true Published\n",
			want:     "my-pkg 1 true Published\n",
		},
		"multi-line with placeholder on second line only": {
			expected: "NAME REVISION AGE\nmy-pkg 1 {{ANY}}\n",
			input:    "NAME REVISION AGE\nmy-pkg 1 5s\n",
			want:     "NAME REVISION AGE\nmy-pkg 1 5s\n",
		},
		"placeholder in header line": {
			expected: "NAME {{ANY}} LIFECYCLE\nmy-pkg true Published\n",
			input:    "NAME LATEST LIFECYCLE\nmy-pkg true Published\n",
			want:     "NAME LATEST LIFECYCLE\nmy-pkg true Published\n",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := replacePlaceholders(tc.expected, tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
