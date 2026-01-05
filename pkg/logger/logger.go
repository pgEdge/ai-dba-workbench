/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package logger provides a simple logging interface with verbosity control
package logger

import (
	"log"
	"os"
)

var (
	verbose = false
)

// SetVerbose enables or disables verbose logging
func SetVerbose(v bool) {
	verbose = v
}

// IsVerbose returns whether verbose logging is enabled
func IsVerbose() bool {
	return verbose
}

// Error logs error messages (always shown)
func Error(v ...interface{}) {
	log.Print(v...)
}

// Errorf logs formatted error messages (always shown)
func Errorf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// Fatal logs a message and exits (always shown)
func Fatal(v ...interface{}) {
	log.Fatal(v...)
}

// Fatalf logs a formatted message and exits (always shown)
func Fatalf(format string, v ...interface{}) {
	log.Fatalf(format, v...)
}

// Info logs informational messages (only shown in verbose mode)
func Info(v ...interface{}) {
	if verbose {
		log.Print(v...)
	}
}

// Infof logs formatted informational messages (only shown in verbose mode)
func Infof(format string, v ...interface{}) {
	if verbose {
		log.Printf(format, v...)
	}
}

// Debug logs debug messages (only shown in verbose mode)
// Use for detailed diagnostic information that is typically not needed
func Debug(v ...interface{}) {
	if verbose {
		log.Print(v...)
	}
}

// Debugf logs formatted debug messages (only shown in verbose mode)
// Use for detailed diagnostic information that is typically not needed
func Debugf(format string, v ...interface{}) {
	if verbose {
		log.Printf(format, v...)
	}
}

// Startup logs startup messages (always shown)
func Startup(v ...interface{}) {
	log.Print(v...)
}

// Startupf logs formatted startup messages (always shown)
func Startupf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// Init initializes the logger
func Init() {
	// Set log output to stdout and configure format
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime)
}
