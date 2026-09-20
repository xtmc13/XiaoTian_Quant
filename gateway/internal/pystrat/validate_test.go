package pystrat

import (
	"strings"
	"testing"
)

const validStrategy = `import math
import json

STRATEGY_MANIFEST = {"name": "t", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
  "params": {}, "risk": {"max_position_pct": 0.5, "stop_loss_pct": 0.05}}

def initialize(context):
    pass

def on_bar(context, bar):
    pass
`

func TestValidateStaticOK(t *testing.T) {
	if issues := ValidateStatic(validStrategy); len(issues) != 0 {
		t.Fatalf("expected pass, got %v", issues)
	}
}

func TestValidateStaticRejectsOffWhitelistImports(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{"import os", "import os\n" + validStrategy, "os"},
		{"import os as alias", "import os as system\n" + validStrategy, "os"},
		{"from os import", "from os import path\n" + validStrategy, "os"},
		{"import socket", "import socket\n" + validStrategy, "socket"},
		{"import requests", "import requests\n" + validStrategy, "requests"},
		{"import urllib", "import urllib.request\n" + validStrategy, "urllib"},
		{"import subprocess", "import subprocess\n" + validStrategy, "subprocess"},
		{"indented import inside function",
			validStrategy + "\ndef helper():\n    import sys\n    return sys\n", "sys"},
		{"indented from-import inside on_bar",
			strings.Replace(validStrategy,
				"def on_bar(context, bar):\n    pass",
				"def on_bar(context, bar):\n    from pathlib import Path\n    pass", 1), "pathlib"},
		{"numpy not whitelisted", "import numpy as np\n" + validStrategy, "numpy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := ValidateStatic(tc.code)
			found := false
			for _, iss := range issues {
				if iss.Code != "IMPORT" || iss.Line <= 0 {
					continue
				}
				if strings.Contains(iss.Message, "\""+tc.want+"\"") || strings.Contains(iss.Message, " "+tc.want+" ") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected IMPORT issue mentioning %q with line, got %v", tc.want, issues)
			}
		})
	}
}

func TestValidateStaticAllowsWhitelistedImports(t *testing.T) {
	code := `import math
import json
import datetime
import collections
import heapq
import itertools
import functools
import statistics
from math import sqrt
from collections import deque

STRATEGY_MANIFEST = {"name": "t", "symbol": "BTC/USDT", "interval": "15m", "direction": "both", "params": {}}

def initialize(context):
    pass

def on_bar(context, bar):
    pass
`
	if issues := ValidateStatic(code); len(issues) != 0 {
		t.Fatalf("expected pass, got %v", issues)
	}
}

func TestValidateStaticRejectsDangerousBuiltins(t *testing.T) {
	for _, snippet := range []string{
		"open('/etc/passwd')",
		"eval('1+1')",
		"exec('x=1')",
		"compile('x', '<s>', 'exec')",
		"input()",
		"globals()",
		"__import__('os')",
		"getattr(x, '__globals__')",
	} {
		code := strings.Replace(validStrategy,
			"def on_bar(context, bar):\n    pass",
			"def on_bar(context, bar):\n    x = 1\n    "+snippet+"\n    pass", 1)
		issues := ValidateStatic(code)
		if len(issues) == 0 {
			t.Fatalf("snippet %q must be rejected", snippet)
		}
	}
}

func TestValidateStaticMissingManifest(t *testing.T) {
	code := strings.Replace(validStrategy, "STRATEGY_MANIFEST", "_MANIFEST", 1)
	issues := ValidateStatic(code)
	if len(issues) == 0 {
		t.Fatal("expected issues for missing manifest")
	}
	if issues[0].Code != "MANIFEST" {
		t.Fatalf("expected MANIFEST issue first, got %+v", issues[0])
	}
}

func TestValidateStaticMissingCallbacks(t *testing.T) {
	noBar := strings.Replace(validStrategy,
		"def on_bar(context, bar):\n    pass\n", "", 1)
	issues := ValidateStatic(noBar)
	if !hasCode(issues, "CALLBACK") {
		t.Fatalf("expected CALLBACK issue, got %v", issues)
	}

	noInit := strings.Replace(validStrategy,
		"def initialize(context):\n    pass\n", "", 1)
	issues = ValidateStatic(noInit)
	if !hasCode(issues, "CALLBACK") {
		t.Fatalf("expected CALLBACK issue for missing initialize, got %v", issues)
	}
}

func TestValidateStaticWrongSignature(t *testing.T) {
	badBar := strings.Replace(validStrategy,
		"def on_bar(context, bar):",
		"def on_bar(ctx, bar):", 1)
	issues := ValidateStatic(badBar)
	if !hasCode(issues, "SIGNATURE") {
		t.Fatalf("expected SIGNATURE issue for on_bar(ctx, bar), got %v", issues)
	}

	badInit := strings.Replace(validStrategy,
		"def initialize(context):",
		"def initialize(ctx, extra):", 1)
	issues = ValidateStatic(badInit)
	if !hasCode(issues, "SIGNATURE") {
		t.Fatalf("expected SIGNATURE issue for initialize(ctx, extra), got %v", issues)
	}

	badOrder := validStrategy + "\ndef on_order(ctx, ord):\n    pass\n"
	issues = ValidateStatic(badOrder)
	if !hasCode(issues, "SIGNATURE") {
		t.Fatalf("expected SIGNATURE issue for on_order(ctx, ord), got %v", issues)
	}

	goodOrder := validStrategy + "\ndef on_order(context, order):\n    pass\n"
	if issues := ValidateStatic(goodOrder); hasCode(issues, "SIGNATURE") {
		t.Fatalf("on_order(context, order) must be accepted, got %v", issues)
	}
}

func hasCode(issues []ValidationIssue, code string) bool {
	for _, iss := range issues {
		if iss.Code == code {
			return true
		}
	}
	return false
}
