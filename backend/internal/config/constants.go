package config

import "time"

const (
	LOCAL_PATH              = "./local_data"
	REMOTE_PATH             = "./remote_data"
	API_PORT                = "8080"
	DefaultJobBufferSize    = 2048
	DefaultDebounceInterval = 500 * time.Millisecond
)
