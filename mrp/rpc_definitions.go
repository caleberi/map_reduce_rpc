package mrp

type DownloadRequest struct {
	Handle Handle
	Data   []byte
	Eof    bool
}

type DownloadReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
}

type HandleRequest struct {
	Handle Handle
}

type HandleReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
	Handle       Handle
}

type StartMapReduceRequest struct {
	Handle Handle
	Plugin string // target plugin name (empty = any worker)
}

type StartMapReduceReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
	Message      string
}

type ChunkInfo struct {
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	Path   string `json:"path"`
	Handle int64  `json:"handle,omitempty"`
}

type FileMetadata struct {
	Handle      Handle      `json:"handle"`
	SourceLog   string      `json:"source_log"`
	SourcePath  string      `json:"source_path"`
	SizeBytes   int64       `json:"size_bytes"`
	ModUnixNano int64       `json:"mod_unix_nano"`
	UploadedAt  string      `json:"uploaded_at"`
	Completed   bool        `json:"completed"`
	RawContent  []byte      `json:"raw_content,omitempty"`
	Chunks      []ChunkInfo `json:"chunks,omitempty"`
}

type MapReduceRequest struct {
	Handle     Handle
	File       FileMetadata
	ChunkIndex int
	ChunkInfo  ChunkInfo
}

type MapReduceReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
	Message      string
}

type MapReduceResultRequest struct {
	Handle      Handle
	ChunkIndex  int
	WorkerAddr  string
	OutputFile  string
	OutputData  string
	GeneratedAt string
}

type MapReduceResultReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
	Message      string
}

type JobCompleteRequest struct {
	Handle     Handle
	ResultFile string
	Status     string
	Message    string
}

type JobCompleteReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
	Message      string
}

type HeartbeatRequest struct {
	WorkerID string
	Message  string
}

type HeartbeatReply struct {
	Status    string
	Message   string
	ServerUTC string
}

type TriggerProcessingRequest struct {
	Plugin string // which plugin to route jobs to
}

type TriggerProcessingReply struct {
	Status            string
	ErrorMessage      string
	HandlesDispatched int
}

type GetJobResultRequest struct {
	HandleId uint64
}

type GetJobResultReply struct {
	Status       string
	ErrorMessage string
	Complete     bool
	ResultJSON   string // raw JSON of the collated result
}

type PluginInfoRequest struct{}

type PluginInfoReply struct {
	PluginName string
}
