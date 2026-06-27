//go:build !linux

package monitor

type processCPUSampler struct{}

func newProcessCPUSampler() *processCPUSampler {
	return &processCPUSampler{}
}

func (s *processCPUSampler) Percent() (*float64, bool) {
	return nil, false
}
