package prober

import "time"

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
	// ReplyTTL is the TTL (IPv4) or hop limit (IPv6) of the echo reply as
	// received, 0 when unknown (e.g. on Windows). See EstimateHops.
	ReplyTTL int `json:"reply_ttl,omitempty"`
}

type HTTPDetails struct {
	StatusCode   int   `json:"status_code"`
	ResponseSize int64 `json:"response_size"`
	// SizeTruncated: the body was larger than ResponseSize, which is a lower bound.
	SizeTruncated bool              `json:"size_truncated,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Redirects     []string          `json:"redirects,omitempty"`
	// RemoteAddr is the IP:port of the connection that served the final
	// response (after redirects).
	RemoteAddr string `json:"remote_addr,omitempty"`
	// TLS describes the final response's TLS connection; nil for plain HTTP.
	TLS *TLSDetails `json:"tls,omitempty"`
}

// TLSDetails summarizes a TLS connection and its leaf certificate. It is
// recorded whether or not the certificate was verified.
type TLSDetails struct {
	Version   string    `json:"version"`              // e.g. "TLS 1.3"
	SubjectCN string    `json:"subject_cn,omitempty"` // leaf certificate subject CN
	Issuer    string    `json:"issuer,omitempty"`     // issuer CN, or the full issuer name without a CN
	NotAfter  time.Time `json:"not_after,omitzero"`   // leaf certificate expiry
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
