package http_test

import (
	"fmt"
	"strings"
)

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func trimNL(s string) string { return strings.TrimRight(s, "\n") }
