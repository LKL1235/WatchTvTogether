package applog

import (
	"log"
	"os"
)

// infoLog writes to stdout so aggregators do not treat routine diagnostics as errors
// (the standard library log package uses stderr).
var infoLog = log.New(os.Stdout, "", log.LstdFlags)

// Infof logs an informational message at info level (stdout).
func Infof(format string, args ...any) {
	infoLog.Printf(format, args...)
}
