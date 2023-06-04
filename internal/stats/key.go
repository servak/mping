package stats

type Key uint8

const (
	Host Key = iota
	Sent
	Success
	Fail
	Loss
	Last
	Avg
	Best
	Worst
	LastSuccTime
	LastFailTime
)

func (s Key) String() string {
	switch s {
	case Host:
		return "Host"
	case Sent:
		return "Sent"
	case Success:
		return "Succ"
	case Loss:
		return "Loss(%)"
	case Last:
		return "Last"
	case Fail:
		return "Fail"
	case Avg:
		return "Avg"
	case Best:
		return "Best"
	case Worst:
		return "Worst"
	case LastSuccTime:
		return "Last Succ"
	case LastFailTime:
		return "Last Fail"
	}

	return ""
}

func Keys() []Key {
	return []Key{Host, Sent, Success, Fail, Loss, Last, Avg, Best, Worst, LastSuccTime, LastFailTime}
}

func KeyStrings() (res []string) {
	for _, k := range Keys() {
		res = append(res, k.String())
	}
	return
}
