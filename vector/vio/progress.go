package vio

import "sync/atomic"

// A Meter provides Progress statistics.
type Meter interface {
	Progress() Progress
}

// Progress represents progress statistics from a Scanner.
type Progress struct {
	BytesScanned              int64 `super:"bytes_scanned" json:"bytes_scanned"`
	ValuesScanned             int64 `super:"values_scanned" json:"values_scanned"`
	FramesScanned             int64 `super:"frames_scanned" json:"frames_scanned"`
	FramesSkippedSearchFilter int64 `super:"frames_skipped_search_filter" json:"frames_skipped_search_filter"`
	FramesSkippedMetaFilter   int64 `super:"frames_skipped_meta_filter" json:"frames_skipped_meta_filter"`
	FramesSkippedDataFilter   int64 `super:"frames_skipped_data_filter" json:"frames_skipped_data_filter"`
	ValuesSkippedDataFilter   int64 `super:"values_skipped_data_filter" json:"values_skipped_data_filter"`
	MetaBytesLoaded           int64 `super:"meta_bytes_loaded" json:"meta_bytes_loaded"`
	DataBytesLoaded           int64 `super:"data_bytes_loaded" json:"data_bytes_loaded"`
	TypesBytesLoaded          int64 `super:"type_bytes_loaded" json:"type_bytes_loaded"`
}

var _ Meter = (*Progress)(nil)

// Add updates its receiver by adding to it the values in ss.
func (p *Progress) Add(in Progress) {
	if p != nil {
		atomic.AddInt64(&p.BytesScanned, in.BytesScanned)
		atomic.AddInt64(&p.ValuesScanned, in.ValuesScanned)
		atomic.AddInt64(&p.FramesScanned, in.FramesScanned)
		atomic.AddInt64(&p.FramesSkippedSearchFilter, in.FramesSkippedSearchFilter)
		atomic.AddInt64(&p.FramesSkippedMetaFilter, in.FramesSkippedMetaFilter)
		atomic.AddInt64(&p.FramesSkippedDataFilter, in.FramesSkippedDataFilter)
		atomic.AddInt64(&p.ValuesSkippedDataFilter, in.ValuesSkippedDataFilter)
		atomic.AddInt64(&p.MetaBytesLoaded, in.MetaBytesLoaded)
		atomic.AddInt64(&p.DataBytesLoaded, in.DataBytesLoaded)
		atomic.AddInt64(&p.TypesBytesLoaded, in.TypesBytesLoaded)
	}
}

func (p *Progress) Copy() Progress {
	if p == nil {
		return Progress{}
	}
	return Progress{
		BytesScanned:              atomic.LoadInt64(&p.BytesScanned),
		ValuesScanned:             atomic.LoadInt64(&p.ValuesScanned),
		FramesScanned:             atomic.LoadInt64(&p.FramesScanned),
		FramesSkippedSearchFilter: atomic.LoadInt64(&p.FramesSkippedSearchFilter),
		FramesSkippedMetaFilter:   atomic.LoadInt64(&p.FramesSkippedMetaFilter),
		FramesSkippedDataFilter:   atomic.LoadInt64(&p.FramesSkippedDataFilter),
		ValuesSkippedDataFilter:   atomic.LoadInt64(&p.ValuesSkippedDataFilter),
		MetaBytesLoaded:           atomic.LoadInt64(&p.MetaBytesLoaded),
		DataBytesLoaded:           atomic.LoadInt64(&p.DataBytesLoaded),
		TypesBytesLoaded:          atomic.LoadInt64(&p.TypesBytesLoaded),
	}
}

func (p *Progress) Progress() Progress {
	return p.Copy()
}
