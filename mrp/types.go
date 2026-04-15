package mrp

const (
	MAPREDUCE_DOWNLOAD_LOG_DIR        = "MAPREDUCE_DOWNLOAD_LOG_DIR"
	MAPREDUCE_MASTER_SERVER_ADDRESS   = "MAPREDUCE_MASTER_SERVER_ADDRESS"
	MAPREDUCE_DFS_UPLOAD_PREFIX       = "MAPREDUCE_DFS_UPLOAD"
	MAPREDUCE_PLUGIN_NAME             = "MAPREDUCE_PLUGIN_NAME"
	MAPREDUCE_PLUGIN_PATH             = "MAPREDUCE_PLUGIN_PATH"
	MAPREDUCE_WORKER_INTERMEDIATE_DIR = "MAPREDUCE_WORKER_INTERMEDIATE_DIR"
	MAPREDUCE_WORKER_OUTPUT_DIR       = "MAPREDUCE_WORKER_OUTPUT_DIR"
	MAPREDUCE_N_REDUCE                = "MAPREDUCE_N_REDUCE"
)

type CompletedHandleMetadata struct {
	Handle      Handle `json:"handle"`
	SourceLog   string `json:"source_log"`
	SourcePath  string `json:"source_path"`
	SizeBytes   int64  `json:"size_bytes"`
	ModUnixNano int64  `json:"mod_unix_nano"`
	UploadedAt  string `json:"uploaded_at"`
	Completed   bool   `json:"completed"`
	RawContent  []byte `json:"raw_content,omitempty"`
}

type MapTask struct {
	fileName string
	index    int
}

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }
