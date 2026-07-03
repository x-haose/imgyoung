package channel

// 副本状态机取值(blob_channels.state):pending → uploading → done / failed。
// 业务表是唯一真相(outbox),异步补齐任务只是幂等触发器。
const (
	// StatePending 待补齐。
	StatePending = "pending"
	// StateUploading 补齐进行中。
	StateUploading = "uploading"
	// StateDone 副本就绪。
	StateDone = "done"
	// StateFailed 补齐失败(达到重试上限)。
	StateFailed = "failed"
)
