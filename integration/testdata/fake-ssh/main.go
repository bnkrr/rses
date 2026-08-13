package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	countPath := requiredEnv("FAKE_SSH_COUNT")
	logPath := requiredEnv("FAKE_SSH_LOG")

	count := 0
	if data, err := os.ReadFile(countPath); err == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	count++
	if err := os.WriteFile(countPath, []byte(strconv.Itoa(count)+"\n"), 0o644); err != nil {
		fatal(err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fatal(err)
	}
	if _, err := fmt.Fprintf(logFile, "fake ssh attempt %d: %s\n", count, strings.Join(os.Args[1:], " ")); err != nil {
		fatal(err)
	}
	if err := logFile.Close(); err != nil {
		fatal(err)
	}

	if count == 1 {
		os.Exit(7)
	}
	time.Sleep(30 * time.Second)
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		fatal(fmt.Errorf("%s is required", name))
	}
	return value
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
