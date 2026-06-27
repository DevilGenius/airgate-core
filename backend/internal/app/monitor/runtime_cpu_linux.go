//go:build linux

package monitor

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const procClockTicksPerSecond = 100.0

type processCPUSampler struct {
	last runtimeCPUSample
	ok   bool
}

type runtimeCPUSample struct {
	at    time.Time
	ticks uint64
}

func newProcessCPUSampler() *processCPUSampler {
	return &processCPUSampler{}
}

func (s *processCPUSampler) Percent() (*float64, bool) {
	current, ok := readRuntimeCPUSample()
	if !ok {
		return nil, false
	}
	if !s.ok {
		s.last = current
		s.ok = true
		return nil, false
	}
	previous := s.last
	s.last = current

	elapsed := current.at.Sub(previous.at).Seconds()
	if elapsed <= 0 || current.ticks < previous.ticks {
		return nil, false
	}
	cpuSeconds := float64(current.ticks-previous.ticks) / procClockTicksPerSecond
	percent := cpuSeconds / elapsed / float64(runtime.NumCPU()) * 100
	if percent < 0 {
		percent = 0
	}
	return &percent, true
}

func readRuntimeCPUSample() (runtimeCPUSample, bool) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return runtimeCPUSample{}, false
	}
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return runtimeCPUSample{}, false
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) <= 12 {
		return runtimeCPUSample{}, false
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return runtimeCPUSample{}, false
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return runtimeCPUSample{}, false
	}
	return runtimeCPUSample{
		at:    time.Now(),
		ticks: utime + stime,
	}, true
}
