package common

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStreamStatus_SetEndReason_FirstWins(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	s.SetEndReason(StreamEndReasonDone, nil)
	s.SetEndReason(StreamEndReasonTimeout, nil)
	s.SetEndReason(StreamEndReasonClientGone, fmt.Errorf("context canceled"))

	assert.Equal(t, StreamEndReasonDone, s.EndReason)
	assert.Nil(t, s.EndError)
}

func TestStreamStatus_SetEndReason_WithError(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	expectedErr := fmt.Errorf("read: connection reset")
	s.SetEndReason(StreamEndReasonScannerErr, expectedErr)

	assert.Equal(t, StreamEndReasonScannerErr, s.EndReason)
	assert.Equal(t, expectedErr, s.EndError)
}

func TestStreamStatus_SetEndReason_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.SetEndReason(StreamEndReasonDone, nil)
}

func TestStreamStatus_SetEndReason_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	reasons := []StreamEndReason{
		StreamEndReasonDone,
		StreamEndReasonTimeout,
		StreamEndReasonClientGone,
		StreamEndReasonScannerErr,
		StreamEndReasonHandlerStop,
		StreamEndReasonEOF,
		StreamEndReasonPanic,
		StreamEndReasonPingFail,
	}

	var wg sync.WaitGroup
	for _, r := range reasons {
		wg.Add(1)
		go func(reason StreamEndReason) {
			defer wg.Done()
			s.SetEndReason(reason, nil)
		}(r)
	}
	wg.Wait()

	assert.NotEqual(t, StreamEndReasonNone, s.EndReason)
}

func TestStreamStatus_RecordError_Basic(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	s.RecordError("bad json")
	s.RecordError("another bad json")
	s.RecordError("client gone")

	assert.True(t, s.HasErrors())
	assert.Equal(t, 3, s.TotalErrorCount())
	assert.Len(t, s.Errors, 3)
}

func TestStreamStatus_RecordError_CapAtMax(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	for i := range 30 {
		s.RecordError(fmt.Sprintf("error_%d", i))
	}

	assert.Equal(t, maxStreamErrorEntries, len(s.Errors))
	assert.Equal(t, 30, s.TotalErrorCount())
}

func TestStreamStatus_RecordError_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.RecordError("should not panic")
}

func TestStreamStatus_RecordError_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.RecordError(fmt.Sprintf("error_%d", idx))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, s.TotalErrorCount())
	assert.LessOrEqual(t, len(s.Errors), maxStreamErrorEntries)
}

func TestStreamStatus_HasErrors_Empty(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()
	assert.False(t, s.HasErrors())
	assert.Equal(t, 0, s.TotalErrorCount())
}

func TestStreamStatus_HasErrors_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.False(t, s.HasErrors())
	assert.Equal(t, 0, s.TotalErrorCount())
}

func TestStreamStatus_IsNormalEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason StreamEndReason
		normal bool
	}{
		{StreamEndReasonDone, true},
		{StreamEndReasonEOF, true},
		{StreamEndReasonHandlerStop, true},
		{StreamEndReasonTimeout, false},
		{StreamEndReasonClientGone, false},
		{StreamEndReasonScannerErr, false},
		{StreamEndReasonPanic, false},
		{StreamEndReasonPingFail, false},
		{StreamEndReasonNone, false},
	}
	for _, tt := range tests {
		s := NewStreamStatus()
		s.SetEndReason(tt.reason, nil)
		assert.Equal(t, tt.normal, s.IsNormalEnd(), "reason=%s", tt.reason)
	}
}

func TestStreamStatus_IsNormalEnd_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.True(t, s.IsNormalEnd())
}

// Failed 决定异常管理是否留存现场：协议失败、传输异常、软错误任一成立都算失败
func TestStreamStatus_Failed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		reason      StreamEndReason
		softError   bool
		protocolErr bool
		failed      bool
	}{
		{"completed", StreamEndReasonDone, false, false, false},
		{"eof without errors", StreamEndReasonEOF, false, false, false},
		{"handler stop without errors", StreamEndReasonHandlerStop, false, false, false},
		{"idle timeout", StreamEndReasonTimeout, false, false, true},
		{"upstream aborted", StreamEndReasonScannerErr, false, false, true},
		{"soft error on a normal end", StreamEndReasonDone, true, false, true},
		{"protocol failure on a normal end", StreamEndReasonDone, false, true, true},
	}
	for _, tt := range tests {
		s := NewStreamStatus()
		s.SetEndReason(tt.reason, nil)
		if tt.softError {
			s.RecordError("soft error")
		}
		if tt.protocolErr {
			s.MarkFailed("cyber_policy", "", 0)
		}
		assert.Equal(t, tt.failed, s.Failed(), "case=%s", tt.name)
	}
}

func TestStreamStatus_Failed_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.False(t, s.Failed())
}

func TestStreamStatus_Summary(t *testing.T) {
	t.Parallel()

	s := NewStreamStatus()
	s.SetEndReason(StreamEndReasonDone, nil)
	summary := s.Summary()
	assert.Contains(t, summary, "reason=done")
	assert.NotContains(t, summary, "soft_errors")

	s2 := NewStreamStatus()
	s2.SetEndReason(StreamEndReasonTimeout, nil)
	s2.RecordError("bad json")
	s2.RecordError("write failed")
	summary2 := s2.Summary()
	assert.Contains(t, summary2, "reason=timeout")
	assert.Contains(t, summary2, "soft_errors=2")
}

func TestStreamStatus_Summary_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.Equal(t, "StreamStatus<nil>", s.Summary())
}

func TestStreamStatus_MaxDataGap(t *testing.T) {
	t.Parallel()
	at := func(sec int) time.Time { return time.Unix(1000+int64(sec), 0) }
	tests := []struct {
		name  string
		steps func(s *StreamStatus)
		want  time.Duration
	}{
		{
			name:  "single upstream line has no gap",
			steps: func(s *StreamStatus) { s.ObserveUpstreamLine(at(5)) },
			want:  0,
		},
		{
			name: "largest silence between upstream lines is kept",
			steps: func(s *StreamStatus) {
				s.ObserveUpstreamLine(at(0))
				s.ObserveUpstreamLine(at(2))
				s.ObserveUpstreamLine(at(7))
				s.ObserveUpstreamLine(at(8))
			},
			want: 5 * time.Second,
		},
		{
			name: "downstream blocking is not counted as upstream silence",
			steps: func(s *StreamStatus) {
				s.ObserveUpstreamLine(at(0))
				s.PauseDataGapTiming()
				s.ResumeDataGapTiming(at(10))
				s.ObserveUpstreamLine(at(12))
			},
			want: 2 * time.Second,
		},
		{
			name: "stream ending while blocked downstream adds no gap",
			steps: func(s *StreamStatus) {
				s.ObserveUpstreamLine(at(0))
				s.PauseDataGapTiming()
				s.FinishDataGapTiming(at(20))
			},
			want: 0,
		},
		{
			name: "stream ending while waiting upstream counts the trailing silence",
			steps: func(s *StreamStatus) {
				s.ObserveUpstreamLine(at(0))
				s.ObserveUpstreamLine(at(1))
				s.FinishDataGapTiming(at(31))
			},
			want: 30 * time.Second,
		},
	}
	for _, tt := range tests {
		s := NewStreamStatus()
		tt.steps(s)
		assert.Equal(t, tt.want, s.MaxDataGap(), "case=%s", tt.name)
	}
}

func TestStreamStatus_MaxDataGap_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.ObserveUpstreamLine(time.Now())
	s.PauseDataGapTiming()
	s.ResumeDataGapTiming(time.Now())
	s.FinishDataGapTiming(time.Now())
	assert.Equal(t, time.Duration(0), s.MaxDataGap())
}
