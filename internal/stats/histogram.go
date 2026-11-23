package stats

import (
	"math"
	"time"
)

// The buckets are the same for all targets, whatever their timeout, so that
// counts can be added up: the first one has the RTTs up to histogramMin, the
// upper bound of each further one is histogramGrowth times that of the one
// before.
const (
	histogramMin    = time.Microsecond
	histogramGrowth = 1.05
)

var logHistogramGrowth = math.Log(histogramGrowth)

// rttHistogram counts RTTs in buckets of equal relative width. A quantile is
// within 2.5% of the exact one above histogramMin, for some 280 buckets up to a
// second and 370 up to a minute.
// The zero value is empty.
type rttHistogram struct {
	counts []uint64 // up to the bucket of the largest RTT
	total  int
}

func rttBucket(rtt time.Duration) int {
	if rtt <= histogramMin {
		return 0
	}
	return 1 + int(math.Log(float64(rtt)/float64(histogramMin))/logHistogramGrowth)
}

// rttOfBucket returns the geometric mean of the bounds of a bucket.
func rttOfBucket(i int) time.Duration {
	if i == 0 {
		return histogramMin
	}
	return time.Duration(float64(histogramMin) * math.Pow(histogramGrowth, float64(i)-0.5))
}

func (h *rttHistogram) add(rtt time.Duration) {
	i := rttBucket(rtt)
	if i >= len(h.counts) {
		h.counts = append(h.counts, make([]uint64, i+1-len(h.counts))...)
	}
	h.counts[i]++
	h.total++
}

// quantile returns the RTT of a rank (from 1) in ascending order.
func (h *rttHistogram) quantile(rank int) time.Duration {
	sum := 0
	for i, c := range h.counts {
		if sum += int(c); sum >= rank {
			return rttOfBucket(i)
		}
	}
	return 0
}

func (h *rttHistogram) clone() rttHistogram {
	return rttHistogram{counts: append([]uint64(nil), h.counts...), total: h.total}
}
