package models

import "time"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type HostOS string

const (
	OSLinux   HostOS = "linux"
	OSWindows HostOS = "windows"
)

type HostStatus string

const (
	StatusPending HostStatus = "pending"
	StatusOnline  HostStatus = "online"
	StatusOffline HostStatus = "offline"
	StatusRemoved HostStatus = "removed"
)

type Host struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	OS                HostOS     `json:"os"`
	Tags              []string   `json:"tags"`
	Status            HostStatus `json:"status"`
	LastSeen          *time.Time `json:"last_seen"`
	CreatedAt         time.Time  `json:"created_at"`
	EnrollmentToken   *string    `json:"enrollment_token,omitempty"`
	EnrollmentExpires *time.Time `json:"enrollment_expires,omitempty"`
}

type CPUSample struct {
	Time      time.Time `json:"time"`
	UsagePct  float64   `json:"usage_pct"`
	Load1     *float64  `json:"load1,omitempty"`
	Load5     *float64  `json:"load5,omitempty"`
	Load15    *float64  `json:"load15,omitempty"`
}

type MemSample struct {
	Time            time.Time `json:"time"`
	TotalBytes      int64     `json:"total_bytes"`
	UsedBytes       int64     `json:"used_bytes"`
	AvailableBytes  int64     `json:"available_bytes"`
	SwapUsedBytes   int64     `json:"swap_used_bytes"`
	SwapTotalBytes  int64     `json:"swap_total_bytes"`
}

type DiskSample struct {
	Time          time.Time `json:"time"`
	Mount         string    `json:"mount"`
	TotalBytes    int64     `json:"total_bytes"`
	UsedBytes     int64     `json:"used_bytes"`
	ReadBytesSec  float64   `json:"read_bytes_sec"`
	WriteBytesSec float64   `json:"write_bytes_sec"`
}

type GPUSample struct {
	Time            time.Time `json:"time"`
	GPUIndex        int       `json:"gpu_index"`
	Vendor          string    `json:"vendor"`
	Name            string    `json:"name"`
	UtilizationPct  *float64  `json:"utilization_pct,omitempty"`
	MemUsedBytes    *int64    `json:"mem_used_bytes,omitempty"`
	MemTotalBytes   *int64    `json:"mem_total_bytes,omitempty"`
	TempC           *float64  `json:"temp_c,omitempty"`
}

type IngestPayload struct {
	CPU  []CPUSample  `json:"cpu,omitempty"`
	Mem  []MemSample  `json:"mem,omitempty"`
	Disk []DiskSample `json:"disk,omitempty"`
	GPU  []GPUSample  `json:"gpu,omitempty"`
}
