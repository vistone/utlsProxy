package src // Package src 定义src包
import "time"

type UTlsRequest struct {
	WorkID      string
	Domain      string
	Method      string
	Path        string
	Headers     map[string]string
	Body        []byte
	DomainIP    string
	LocalIP     string
	Fingerprint Profile
	StartTime   time.Time
}
type UTlsResponse struct {
	WorkID     string
	StatusCode int
	Body       []byte
	Duration   time.Duration
}
