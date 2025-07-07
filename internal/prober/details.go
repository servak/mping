package prober

// プローブ詳細情報（TargetIPを削除、TCPDetailsも削除）
type ProbeDetails struct {
	ProbeType string `json:"probe_type"`

	// 型別詳細（どれか一つのみ使用）
	ICMP *ICMPDetails `json:"icmp,omitempty"`
	HTTP *HTTPDetails `json:"http,omitempty"`
	DNS  *DNSDetails  `json:"dns,omitempty"`
	NTP  *NTPDetails  `json:"ntp,omitempty"`
	// TCP は詳細情報なし（接続可否のみのため）
}

type ICMPDetails struct {
	Sequence int `json:"sequence"`
	TTL      int `json:"ttl"`
	DataSize int `json:"data_size"`
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
	Offset    int64  `json:"offset_microseconds"` // マイクロ秒単位
	Precision int    `json:"precision"`
}