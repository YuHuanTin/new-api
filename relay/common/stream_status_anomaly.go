package common

// Failed reports whether the request should be treated as failed: the protocol
// reported a failure, the transport ended abnormally (timeout, aborted upstream,
// failed ping), or soft errors were recorded along the way. The anomaly capture
// uses it to decide whether to keep the request evidence
func (s *StreamStatus) Failed() bool {
	if s == nil {
		return false
	}
	return !s.IsNormalEnd() || s.HasErrors() || s.ResponseFailed()
}
