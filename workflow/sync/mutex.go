package sync

func NewMutex(name string, nextWorkflow NextWorkflow) *prioritySemaphore {
	return NewSemaphore(name, 1, nextWorkflow, "mutex")
}
