package prober

// Probe detail information (TargetIP and TCPDetails removed)
type ProbeDetails struct {
	ProbeType string `json:"probe_type"`

	// Type-specific details (only one should be used)
	ICMP *ICMPDetails `json:"icmp,omitempty"`
	HTTP *HTTPDetails `json:"http,omitempty"`
	DNS  *DNSDetails  `json:"dns,omitempty"`
	NTP  *NTPDetails  `json:"ntp,omitempty"`
	// TCP has no detailed information (only connection availability)
}

type ICMPDetails struct {
	Sequence   int    `json:"sequence"`
	PacketSize int    `json:"packet_size"`
	ICMPType   int    `json:"icmp_type"`
	ICMPCode   int    `json:"icmp_code"`
	Checksum   uint16 `json:"checksum"`
	Payload    string `json:"payload"` // Actual payload content with length limit
}

type HTTPDetails struct {
	StatusCode   int               `json:"status_code"`
	ResponseSize int64             `json:"response_size"`
	Headers      map[string]string `json:"headers,omitempty"`
	Redirects    []string          `json:"redirects,omitempty"`
}

type DNSDetails struct {
	Server       string   `json:"server"`
	Port         int      `json:"port"`
	Domain       string   `json:"domain"`
	RecordType   string   `json:"record_type"`
	ResponseCode int      `json:"response_code"`
	AnswerCount  int      `json:"answer_count"`
	Answers      []string `json:"answers,omitempty"`
	UseTCP       bool     `json:"use_tcp"`
}

type NTPDetails struct {
	Server    string `json:"server"`
	Port      int    `json:"port"`
	Stratum   int    `json:"stratum"`
	Offset    int64  `json:"offset_microseconds"` // In microseconds
	Precision int    `json:"precision"`
}