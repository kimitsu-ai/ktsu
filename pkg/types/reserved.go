package types

// ReservedFields holds the rss_* output fields that the orchestrator
// interprets for control flow and routing decisions.
// These fields are reserved and may not be set by user pipeline steps.
type ReservedFields struct {
	// rss_next specifies the next step ID to execute (overrides DAG default)
	Next string `json:"rss_next,omitempty"`
	// rss_skip is a list of step IDs to skip
	Skip []string `json:"rss_skip,omitempty"`
	// rss_abort signals that the pipeline should be aborted
	Abort bool `json:"rss_abort,omitempty"`
	// rss_abort_reason is a human-readable reason for aborting
	AbortReason string `json:"rss_abort_reason,omitempty"`
	// rss_retry requests that the current step be retried
	Retry bool `json:"rss_retry,omitempty"`
	// rss_retry_delay_ms is the delay before retrying in milliseconds
	RetryDelayMS int `json:"rss_retry_delay_ms,omitempty"`
}

// ReservedPrefix is the prefix for all reserved output fields
const ReservedPrefix = "rss_"
