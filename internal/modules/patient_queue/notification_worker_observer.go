package patient_queue

import "time"

// WorkerLoopObserver records worker Tick / claim / stale-recovery signals (LOT 26I-5B).
// Implementations must not return errors or panic; observation never fails business work.
// No Prometheus types, IDs, error strings, or unbounded labels.
type WorkerLoopObserver interface {
	// ObserveTick records one Tick wall-clock duration and result (err==nil → success).
	ObserveTick(duration time.Duration, err error)
	// ObserveClaimed records intents successfully transitioned to PROCESSING (n may be 0).
	ObserveClaimed(n int)
	// ObserveStaleRecovered records successful stale recovery transitions (n may be 0;
	// include partial n when recovery returns (n, err)).
	ObserveStaleRecovered(n int)
}

// noopWorkerLoopObserver discards all observations.
type noopWorkerLoopObserver struct{}

func (noopWorkerLoopObserver) ObserveTick(time.Duration, error) {}
func (noopWorkerLoopObserver) ObserveClaimed(int)               {}
func (noopWorkerLoopObserver) ObserveStaleRecovered(int)        {}
